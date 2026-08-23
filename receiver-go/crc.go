package main

import "hash/crc32"

type decodeResult struct {
	payload            []byte
	detectedUnits      uint64
	correctedBits      uint64
	uncorrectableUnits uint64
	errorCode          string
	detail             string
}

func decodeCRC(frame string) decodeResult {
	payloadBitLength := len(frame) - 32
	payload := bitsToBytes(frame[:payloadBitLength])
	var received uint32
	for _, bit := range []byte(frame[payloadBitLength:]) {
		received = (received << 1) | uint32(bit-'0')
	}
	if crc32.ChecksumIEEE(payload) != received {
		return decodeResult{
			payload:            payload,
			detectedUnits:      1,
			uncorrectableUnits: 1,
			errorCode:          "integrity_check_failed",
			detail:             "CRC-32/ISO-HDLC checksum mismatch",
		}
	}
	return decodeResult{payload: payload}
}

func bitsToBytes(bits string) []byte {
	result := make([]byte, len(bits)/8)
	for index, bit := range []byte(bits) {
		result[index/8] = (result[index/8] << 1) | (bit - '0')
	}
	return result
}
