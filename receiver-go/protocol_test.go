package main

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"strings"
	"testing"
)

type normativeVectors struct {
	SchemaVersion             int             `json:"schema_version"`
	ASCIIVectors              []asciiVector   `json:"ascii_vectors"`
	CRCVectors                []crcVector     `json:"crc32_iso_hdlc_vectors"`
	HammingVectors            []hammingVector `json:"hamming_secded_13_8_vectors"`
	DeterministicNoiseVectors []noiseVector   `json:"deterministic_noise_vectors"`
}

type asciiVector struct {
	InputText                        string `json:"input_text"`
	ExpectedBitsMostSignificantFirst string `json:"expected_bits_most_significant_first"`
}

type crcVector struct {
	InputText              string `json:"input_text"`
	ExpectedCRCDecimal     uint32 `json:"expected_crc_decimal"`
	ExpectedCleanFrameBits string `json:"expected_clean_frame_bits"`
	ExpectedReceiverStatus string `json:"expected_receiver_status"`
	ExpectedDecodedText    string `json:"expected_decoded_text"`
}

type hammingVector struct {
	ReceivedCodeword       string  `json:"received_codeword_positions_1_through_13"`
	ExpectedReceiverStatus string  `json:"expected_receiver_status"`
	ExpectedDecodedText    *string `json:"expected_decoded_text"`
	ExpectedErrorCode      string  `json:"expected_error_code"`
}

type noiseVector struct {
	ExpectedNoisyFrameBits string `json:"expected_noisy_frame_bits"`
	ExpectedFlippedBits    uint64 `json:"expected_flipped_bits_count"`
}

func loadVectors(t *testing.T) normativeVectors {
	t.Helper()
	data, err := os.ReadFile("../protocol/test-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors normativeVectors
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	return vectors
}

func TestNormativeASCIIAndCRCVectors(t *testing.T) {
	vectors := loadVectors(t)
	if vectors.SchemaVersion != 1 {
		t.Fatalf("schema version = %d", vectors.SchemaVersion)
	}
	for _, vector := range vectors.ASCIIVectors {
		if got := bytesToBits([]byte(vector.InputText)); got != vector.ExpectedBitsMostSignificantFirst {
			t.Errorf("ASCII bits = %s, want %s", got, vector.ExpectedBitsMostSignificantFirst)
		}
	}
	for _, vector := range vectors.CRCVectors {
		t.Run(vector.InputText, func(t *testing.T) {
			if got := crc32.ChecksumIEEE([]byte(vector.InputText)); got != vector.ExpectedCRCDecimal {
				t.Fatalf("CRC = %08X, want %08X", got, vector.ExpectedCRCDecimal)
			}
			response := processLine(validRequest("crc-vector", "crc32-iso-hdlc", uint64(len(vector.InputText)), vector.ExpectedCleanFrameBits, 0))
			assertResponse(t, response, vector.ExpectedReceiverStatus, "", vector.ExpectedDecodedText)
		})
	}
}

func TestNormativeHammingAndNoiseVectors(t *testing.T) {
	vectors := loadVectors(t)
	for index, vector := range vectors.HammingVectors {
		t.Run(fmt.Sprintf("hamming-%d", index), func(t *testing.T) {
			response := processLine(validRequest("hamming-vector", "hamming-secded-13-8", 1, vector.ReceivedCodeword, 0))
			message := ""
			if vector.ExpectedDecodedText != nil {
				message = *vector.ExpectedDecodedText
			}
			assertResponse(t, response, vector.ExpectedReceiverStatus, vector.ExpectedErrorCode, message)
		})
	}
	for _, vector := range vectors.DeterministicNoiseVectors {
		response := processLine(validRequest("noise-vector", "hamming-secded-13-8", 1, vector.ExpectedNoisyFrameBits, vector.ExpectedFlippedBits))
		assertResponse(t, response, "corrected", "", "A")
		if response.Metrics.ReportedFlippedBits != vector.ExpectedFlippedBits {
			t.Fatalf("reported flips = %d, want %d", response.Metrics.ReportedFlippedBits, vector.ExpectedFlippedBits)
		}
	}
}

func TestCRCIntegrityAndASCIIValidation(t *testing.T) {
	tests := []struct {
		name       string
		frame      string
		wantCode   string
		wantStatus string
	}{
		{"altered frame", flipBit(crcFrame([]byte("A")), 0), "integrity_check_failed", "detected_uncorrectable"},
		{"valid CRC with invalid ASCII", crcFrame([]byte{0x1f}), "invalid_ascii_payload", "detected_uncorrectable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := processLine(validRequest("crc-case", "crc32-iso-hdlc", 1, test.frame, 1))
			assertResponse(t, response, test.wantStatus, test.wantCode, "")
		})
	}
}

func TestHammingCorrectionUncorrectableAndASCIIValidation(t *testing.T) {
	tests := []struct {
		name        string
		frame       string
		wantStatus  string
		wantCode    string
		wantMessage string
	}{
		{"clean", "1000100100010", "ok", "", "A"},
		{"single data error", "1000110100010", "corrected", "", "A"},
		{"single overall parity error", flipBit("1000100100010", 12), "corrected", "", "A"},
		{"double error", "1010000100010", "detected_uncorrectable", "uncorrectable_error", ""},
		{"clean non-printable payload", encodeHammingByte(0x1f), "detected_uncorrectable", "invalid_ascii_payload", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := processLine(validRequest("hamming-case", "hamming-secded-13-8", 1, test.frame, 1))
			assertResponse(t, response, test.wantStatus, test.wantCode, test.wantMessage)
		})
	}
}

func TestRejectsInvalidJSONSchemaVersionRangesAndLengths(t *testing.T) {
	valid := string(validRequest("valid-1", "hamming-secded-13-8", 1, "1000100100010", 0))
	tests := []struct {
		name string
		line string
		code string
	}{
		{"malformed JSON", `{`, "invalid_json"},
		{"array", `[]`, "invalid_json"},
		{"multiple values", `{} {}`, "invalid_json"},
		{"unknown key", strings.Replace(valid, `"noise":`, `"extra":1,"noise":`, 1), "invalid_schema"},
		{"unknown nested key", strings.Replace(valid, `"seed":0,`, `"extra":0,"seed":0,`, 1), "invalid_schema"},
		{"duplicate key", strings.Replace(valid, `"request_id":"valid-1",`, `"request_id":"valid-1","request_id":"other",`, 1), "invalid_schema"},
		{"duplicate nested key", strings.Replace(valid, `"seed":0,`, `"seed":0,"seed":1,`, 1), "invalid_schema"},
		{"missing field", strings.Replace(valid, `"protocol_version":1,`, "", 1), "invalid_schema"},
		{"wrong type", strings.Replace(valid, `"source_octets":1`, `"source_octets":"1"`, 1), "invalid_schema"},
		{"floating integer", strings.Replace(valid, `"source_octets":1`, `"source_octets":1.0`, 1), "invalid_schema"},
		{"unsupported version", strings.Replace(valid, `"protocol_version":1`, `"protocol_version":2`, 1), "unsupported_version"},
		{"invalid request ID", strings.Replace(valid, `"valid-1"`, `"bad id"`, 1), "invalid_schema"},
		{"zero source length", strings.Replace(valid, `"source_octets":1`, `"source_octets":0`, 1), "invalid_schema"},
		{"invalid bit", strings.Replace(valid, `1000100100010`, `100010010001x`, 1), "invalid_schema"},
		{"invalid probability", strings.Replace(valid, `"probability_numerator":0`, `"probability_numerator":2`, 1), "invalid_schema"},
		{"denominator above limit", strings.Replace(valid, `"probability_denominator":1`, `"probability_denominator":1000000001`, 1), "invalid_schema"},
		{"flip count too large", strings.Replace(valid, `"flipped_bits":0`, `"flipped_bits":14`, 1), "invalid_schema"},
		{"invalid frame length", strings.Replace(valid, `1000100100010`, `100010010001`, 1), "invalid_frame_length"},
		{"request ID above limit", strings.Replace(valid, `"valid-1"`, `"`+strings.Repeat("a", 65)+`"`, 1), "invalid_schema"},
		{"frame above limit", string(validRequest("large-frame", "hamming-secded-13-8", 1, strings.Repeat("0", maxFrameBits+1), 0)), "invalid_schema"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := processLine([]byte(test.line))
			assertResponse(t, response, "invalid_request", test.code, "")
			if response.Metrics != nil {
				t.Fatal("invalid request must not include metrics")
			}
		})
	}
}

func TestAcceptsInclusiveNumericAndFrameBounds(t *testing.T) {
	frame := strings.Repeat("0", maxFrameBits)
	line := validRequest(strings.Repeat("a", 64), "crc32-iso-hdlc", 124_996, frame, uint64(len(frame)))
	line = []byte(strings.Replace(string(line),
		`"probability_numerator":0,"probability_denominator":1,"seed":0`,
		`"probability_numerator":1000000000,"probability_denominator":1000000000,"seed":18446744073709551615`, 1))
	request, requestID, protocolErr := parseRequest(line)
	if protocolErr != nil {
		t.Fatalf("inclusive bounds rejected: %+v", protocolErr)
	}
	if requestID == nil || len(*requestID) != 64 {
		t.Fatalf("request ID was not recovered: %v", requestID)
	}
	if len(request.FrameBits) != maxFrameBits || request.Noise.Seed != ^uint64(0) {
		t.Fatalf("parsed bounds incorrectly: frame=%d seed=%d", len(request.FrameBits), request.Noise.Seed)
	}
}

func assertResponse(t *testing.T, response Response, status, code, message string) {
	t.Helper()
	if response.Status != status {
		t.Fatalf("status = %q, want %q; response = %+v", response.Status, status, response)
	}
	if code == "" {
		if response.Error != nil {
			t.Fatalf("unexpected error: %+v", response.Error)
		}
	} else if response.Error == nil || response.Error.Code != code {
		t.Fatalf("error = %+v, want code %q", response.Error, code)
	}
	if message == "" {
		if response.Message != nil {
			t.Fatalf("message = %q, want null", *response.Message)
		}
	} else if response.Message == nil || *response.Message != message {
		t.Fatalf("message = %v, want %q", response.Message, message)
	}
}

func validRequest(requestID, algorithm string, sourceOctets uint64, frame string, flipped uint64) []byte {
	return []byte(fmt.Sprintf(`{"protocol_version":1,"request_id":%q,"algorithm":%q,"source_octets":%d,"frame_bits":%q,"noise":{"probability_numerator":0,"probability_denominator":1,"seed":0,"flipped_bits":%d}}`, requestID, algorithm, sourceOctets, frame, flipped))
}

func bytesToBits(payload []byte) string {
	var builder strings.Builder
	for _, value := range payload {
		fmt.Fprintf(&builder, "%08b", value)
	}
	return builder.String()
}

func crcFrame(payload []byte) string {
	return bytesToBits(payload) + fmt.Sprintf("%032b", crc32.ChecksumIEEE(payload))
}

func flipBit(bits string, index int) string {
	result := []byte(bits)
	if result[index] == '0' {
		result[index] = '1'
	} else {
		result[index] = '0'
	}
	return string(result)
}

func encodeHammingByte(value byte) string {
	positions := [14]byte{}
	for index, position := range dataPositions {
		positions[position] = (value >> (7 - index)) & 1
	}
	for _, position := range parityPositions {
		positions[position] = parityXOR(positions, position)
	}
	for position := 1; position <= 12; position++ {
		positions[13] ^= positions[position]
	}
	var builder strings.Builder
	for position := 1; position <= 13; position++ {
		builder.WriteByte('0' + positions[position])
	}
	return builder.String()
}
