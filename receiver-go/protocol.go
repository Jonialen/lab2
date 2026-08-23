package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

const (
	protocolVersion = 1
	maxFrameBits    = 1_000_000
)

type Request struct {
	ProtocolVersion uint64
	RequestID       string
	Algorithm       string
	SourceOctets    uint64
	FrameBits       string
	Noise           Noise
}

type Noise struct {
	ProbabilityNumerator   uint64
	ProbabilityDenominator uint64
	Seed                   uint64
	FlippedBits            uint64
}

type wireRequest struct {
	ProtocolVersion *json.RawMessage `json:"protocol_version"`
	RequestID       *string          `json:"request_id"`
	Algorithm       *string          `json:"algorithm"`
	SourceOctets    *json.RawMessage `json:"source_octets"`
	FrameBits       *string          `json:"frame_bits"`
	Noise           *wireNoise       `json:"noise"`
}

type wireNoise struct {
	ProbabilityNumerator   *json.RawMessage `json:"probability_numerator"`
	ProbabilityDenominator *json.RawMessage `json:"probability_denominator"`
	Seed                   *json.RawMessage `json:"seed"`
	FlippedBits            *json.RawMessage `json:"flipped_bits"`
}

type Response struct {
	ProtocolVersion int            `json:"protocol_version"`
	RequestID       *string        `json:"request_id"`
	Status          string         `json:"status"`
	Message         *string        `json:"message"`
	Error           *ResponseError `json:"error"`
	Metrics         *Metrics       `json:"metrics"`
}

type ResponseError struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type Metrics struct {
	ReceivedBits        uint64 `json:"received_bits"`
	SourceBits          uint64 `json:"source_bits"`
	RedundancyBits      uint64 `json:"redundancy_bits"`
	ReportedFlippedBits uint64 `json:"reported_flipped_bits"`
	DetectedUnits       uint64 `json:"detected_units"`
	CorrectedBits       uint64 `json:"corrected_bits"`
	UncorrectableUnits  uint64 `json:"uncorrectable_units"`
}

type protocolError struct {
	code   string
	detail string
}

func processLine(line []byte) Response {
	request, requestID, err := parseRequest(line)
	if err != nil {
		return invalidResponse(requestID, err.code, err.detail)
	}
	return decodeRequest(request)
}

func parseRequest(line []byte) (Request, *string, *protocolError) {
	if len(line) == 0 {
		return Request{}, nil, &protocolError{"invalid_json", "request line must contain one JSON object"}
	}
	if err := validateJSONShape(line); err != nil {
		code := "invalid_json"
		if errors.Is(err, errDuplicateKey) {
			code = "invalid_schema"
		}
		return Request{}, nil, &protocolError{code, err.Error()}
	}

	requestID := recoverRequestID(line)
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var wire wireRequest
	if err := decoder.Decode(&wire); err != nil {
		return Request{}, requestID, &protocolError{"invalid_schema", "request does not match the exact schema"}
	}
	if wire.ProtocolVersion == nil || wire.RequestID == nil || wire.Algorithm == nil ||
		wire.SourceOctets == nil || wire.FrameBits == nil || wire.Noise == nil ||
		wire.Noise.ProbabilityNumerator == nil || wire.Noise.ProbabilityDenominator == nil ||
		wire.Noise.Seed == nil || wire.Noise.FlippedBits == nil {
		return Request{}, requestID, &protocolError{"invalid_schema", "all request and noise fields are required and non-null"}
	}

	version, ok := unsignedInteger(*wire.ProtocolVersion)
	if !ok {
		return Request{}, requestID, &protocolError{"invalid_schema", "protocol_version must be a non-negative JSON integer"}
	}
	sourceOctets, ok := unsignedInteger(*wire.SourceOctets)
	if !ok {
		return Request{}, requestID, &protocolError{"invalid_schema", "source_octets must be a non-negative JSON integer"}
	}
	numerator, numeratorOK := unsignedInteger(*wire.Noise.ProbabilityNumerator)
	denominator, denominatorOK := unsignedInteger(*wire.Noise.ProbabilityDenominator)
	seed, seedOK := unsignedInteger(*wire.Noise.Seed)
	flippedBits, flippedOK := unsignedInteger(*wire.Noise.FlippedBits)
	if !numeratorOK || !denominatorOK || !seedOK || !flippedOK {
		return Request{}, requestID, &protocolError{"invalid_schema", "noise values must be non-negative JSON integers"}
	}

	request := Request{
		ProtocolVersion: version,
		RequestID:       *wire.RequestID,
		Algorithm:       *wire.Algorithm,
		SourceOctets:    sourceOctets,
		FrameBits:       *wire.FrameBits,
		Noise: Noise{
			ProbabilityNumerator:   numerator,
			ProbabilityDenominator: denominator,
			Seed:                   seed,
			FlippedBits:            flippedBits,
		},
	}
	if version != protocolVersion {
		return Request{}, requestID, &protocolError{"unsupported_version", "protocol_version must be 1"}
	}
	if !validRequestID(request.RequestID) {
		return Request{}, nil, &protocolError{"invalid_schema", "request_id must match [A-Za-z0-9._-]{1,64}"}
	}
	requestID = &request.RequestID
	if request.Algorithm != "crc32-iso-hdlc" && request.Algorithm != "hamming-secded-13-8" {
		return Request{}, requestID, &protocolError{"invalid_schema", "algorithm is not supported"}
	}
	if request.SourceOctets == 0 {
		return Request{}, requestID, &protocolError{"invalid_schema", "source_octets must be at least 1"}
	}
	if len(request.FrameBits) > maxFrameBits || !onlyBits(request.FrameBits) {
		return Request{}, requestID, &protocolError{"invalid_schema", "frame_bits must contain at most 1,000,000 binary digits"}
	}
	if denominator == 0 || denominator > 1_000_000_000 || numerator > denominator {
		return Request{}, requestID, &protocolError{"invalid_schema", "noise probability is outside the allowed range"}
	}
	if flippedBits > uint64(len(request.FrameBits)) {
		return Request{}, requestID, &protocolError{"invalid_schema", "noise.flipped_bits exceeds frame length"}
	}
	if !validFrameLength(request) {
		return Request{}, requestID, &protocolError{"invalid_frame_length", "frame length does not match algorithm and source_octets"}
	}
	return request, requestID, nil
}

func decodeRequest(request Request) Response {
	metrics := Metrics{
		ReceivedBits:        uint64(len(request.FrameBits)),
		SourceBits:          request.SourceOctets * 8,
		ReportedFlippedBits: request.Noise.FlippedBits,
	}
	var result decodeResult
	if request.Algorithm == "crc32-iso-hdlc" {
		metrics.RedundancyBits = 32
		result = decodeCRC(request.FrameBits)
	} else {
		metrics.RedundancyBits = request.SourceOctets * 5
		result = decodeHamming(request.FrameBits)
	}
	metrics.DetectedUnits = result.detectedUnits
	metrics.CorrectedBits = result.correctedBits
	metrics.UncorrectableUnits = result.uncorrectableUnits

	if result.errorCode != "" {
		return failureResponse(request.RequestID, result.errorCode, result.detail, metrics)
	}
	if !printableASCII(result.payload) {
		return failureResponse(request.RequestID, "invalid_ascii_payload", "recovered payload is not printable ASCII", metrics)
	}
	message := string(result.payload)
	status := "ok"
	if result.correctedBits > 0 {
		status = "corrected"
	}
	return Response{protocolVersion, &request.RequestID, status, &message, nil, &metrics}
}

func invalidResponse(requestID *string, code, detail string) Response {
	return Response{protocolVersion, requestID, "invalid_request", nil, &ResponseError{code, detail}, nil}
}

func failureResponse(requestID, code, detail string, metrics Metrics) Response {
	return Response{protocolVersion, &requestID, "detected_uncorrectable", nil, &ResponseError{code, detail}, &metrics}
}

func validRequestID(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, character := range []byte(value) {
		if !(character >= 'A' && character <= 'Z') && !(character >= 'a' && character <= 'z') &&
			!(character >= '0' && character <= '9') && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func onlyBits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range []byte(value) {
		if character != '0' && character != '1' {
			return false
		}
	}
	return true
}

func printableASCII(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	for _, value := range payload {
		if value < 0x20 || value > 0x7e {
			return false
		}
	}
	return true
}

func validFrameLength(request Request) bool {
	var expected uint64
	if request.Algorithm == "crc32-iso-hdlc" {
		if request.SourceOctets > (math.MaxUint64-32)/8 {
			return false
		}
		expected = request.SourceOctets*8 + 32
	} else {
		if request.SourceOctets > math.MaxUint64/13 {
			return false
		}
		expected = request.SourceOctets * 13
	}
	return expected == uint64(len(request.FrameBits))
}

func unsignedInteger(raw json.RawMessage) (uint64, bool) {
	text := string(raw)
	if text == "" || text[0] == '-' {
		return 0, false
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(text, 10, 64)
	return value, err == nil
}

func recoverRequestID(line []byte) *string {
	var object map[string]json.RawMessage
	if json.Unmarshal(line, &object) != nil {
		return nil
	}
	var requestID string
	if raw, exists := object["request_id"]; !exists || json.Unmarshal(raw, &requestID) != nil || !validRequestID(requestID) {
		return nil
	}
	return &requestID
}

var errDuplicateKey = errors.New("duplicate JSON object key")

func validateJSONShape(line []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("request is not valid JSON: %w", err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return errors.New("request JSON value must be an object")
	}
	if err := consumeObject(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request line contains multiple JSON values")
		}
		return fmt.Errorf("request is not valid JSON: %w", err)
	}
	return nil
}

func consumeObject(decoder *json.Decoder) error {
	keys := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("request is not valid JSON: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("request object key must be a string")
		}
		if _, exists := keys[key]; exists {
			return fmt.Errorf("%w: %q", errDuplicateKey, key)
		}
		keys[key] = struct{}{}
		if err := consumeValue(decoder); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("request is not valid JSON: %w", err)
	}
	return nil
}

func consumeValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("request is not valid JSON: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		return consumeObject(decoder)
	case '[':
		for decoder.More() {
			if err := consumeValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}
