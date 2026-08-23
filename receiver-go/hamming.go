package main

var dataPositions = [...]int{3, 5, 6, 7, 9, 10, 11, 12}
var parityPositions = [...]int{1, 2, 4, 8}

func decodeHamming(frame string) decodeResult {
	result := decodeResult{payload: make([]byte, 0, len(frame)/13)}
	for offset := 0; offset < len(frame); offset += 13 {
		positions := [14]byte{}
		for index, bit := range []byte(frame[offset : offset+13]) {
			positions[index+1] = bit - '0'
		}

		syndrome := 0
		for _, parityPosition := range parityPositions {
			if parityXOR(positions, parityPosition) != 0 {
				syndrome |= parityPosition
			}
		}
		overallMismatch := byte(0)
		for position := 1; position <= 13; position++ {
			overallMismatch ^= positions[position]
		}
		if syndrome != 0 || overallMismatch != 0 {
			result.detectedUnits++
		}

		switch {
		case syndrome == 0 && overallMismatch == 0:
		case syndrome == 0 && overallMismatch == 1:
			positions[13] ^= 1
			result.correctedBits++
		case syndrome >= 1 && syndrome <= 12 && overallMismatch == 1:
			positions[syndrome] ^= 1
			result.correctedBits++
		default:
			result.uncorrectableUnits++
		}

		var value byte
		for _, position := range dataPositions {
			value = (value << 1) | positions[position]
		}
		result.payload = append(result.payload, value)
	}
	if result.uncorrectableUnits > 0 {
		result.errorCode = "uncorrectable_error"
		result.detail = "one or more SECDED codewords contain an uncorrectable error"
	}
	return result
}

func parityXOR(positions [14]byte, parityPosition int) byte {
	var parity byte
	for position := 1; position <= 12; position++ {
		if position&parityPosition != 0 {
			parity ^= positions[position]
		}
	}
	return parity
}
