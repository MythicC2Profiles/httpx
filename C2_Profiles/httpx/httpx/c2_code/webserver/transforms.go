package webserver

import (
	"encoding/base64"
	"errors"
)

func transformBase64(prev []byte, value string) ([]byte, error) {
	return []byte(base64.StdEncoding.EncodeToString(prev)), nil
}
func transformBase64Reverse(prev []byte, value string) ([]byte, error) {
	decodedLength := base64.StdEncoding.DecodedLen(len(prev))
	decoded := make([]byte, decodedLength)
	actualDecoded, err := base64.StdEncoding.Decode(decoded, prev)
	if err != nil {
		return nil, err
	}
	return decoded[:actualDecoded], nil
}

func transformBase64URL(prev []byte, value string) ([]byte, error) {
	return []byte(base64.URLEncoding.EncodeToString(prev)), nil
}
func transformBase64URLReverse(prev []byte, value string) ([]byte, error) {
	decodedLength := base64.URLEncoding.DecodedLen(len(prev))
	decoded := make([]byte, decodedLength)
	actualDecoded, err := base64.URLEncoding.Decode(decoded, prev)
	if err != nil {
		return nil, err
	}
	return decoded[:actualDecoded], nil
}

func transformPrepend(prev []byte, value string) ([]byte, error) {
	return append([]byte(value), prev...), nil
}
func transformPrependReverse(prev []byte, value string) ([]byte, error) {
	if len(value) > len(prev) {
		return nil, errors.New("prepend value is longer that full value")
	}
	return prev[len(value):], nil
}

func transformAppend(prev []byte, value string) ([]byte, error) {
	return append(prev, []byte(value)...), nil
}
func transformAppendReverse(prev []byte, value string) ([]byte, error) {
	if len(value) > len(prev) {
		return nil, errors.New("append value is longer that full value")
	}
	return prev[:len(prev)-len(value)], nil
}

func transformXor(prev []byte, value string) ([]byte, error) {
	output := make([]byte, len(prev))
	for i := 0; i < len(prev); i++ {
		output[i] = prev[i] ^ value[i%len(value)]
	}
	return output, nil
}
func transformXorReverse(prev []byte, value string) ([]byte, error) {
	return transformXor(prev, value)
}

func transformNetbios(prev []byte, value string) ([]byte, error) {
	// split each byte into two nibbles
	// pad each nibble out to a byte with zeros
	// add 'a' (0x61)
	output := make([]byte, len(prev)*2)
	for i := 0; i < len(prev); i++ {
		right := (prev[i] & 0x0F) + 0x61
		left := ((prev[i] & 0xF0) >> 4) + 0x61
		output[i*2] = left
		output[(i*2)+1] = right
	}
	return output, nil
}
func transformNetbiosReverse(prev []byte, value string) ([]byte, error) {
	output := make([]byte, len(prev)/2)
	for i := 0; i < len(output); i++ {
		left := (prev[i*2] - 0x61) << 4
		right := prev[i*2+1] - 0x61
		output[i] = left | right
	}
	return output, nil
}

func transformNetbiosu(prev []byte, value string) ([]byte, error) {
	// split each byte into two nibbles
	// pad each nibble out to a byte with zeros
	// add 'a' (0x61)
	output := make([]byte, len(prev)*2)
	for i := 0; i < len(prev); i++ {
		right := (prev[i] & 0x0F) + 0x41
		left := ((prev[i] & 0xF0) >> 4) + 0x41
		output[i*2] = left
		output[(i*2)+1] = right
	}
	return output, nil
}
func transformNetbiosuReverse(prev []byte, value string) ([]byte, error) {
	output := make([]byte, len(prev)/2)
	for i := 0; i < len(output); i++ {
		left := (prev[i*2] - 0x41) << 4
		right := prev[i*2+1] - 0x41
		output[i] = left | right
	}
	return output, nil
}
