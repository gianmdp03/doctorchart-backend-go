package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Spring's external .properties format uses ISO-8859-1 with Java escapes,
// logical line continuation, comments, and escaped key separators.
func readProperties(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b)
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(runes), "\r\n", "\n"), "\r", "\n")
	properties := map[string]string{}
	logical := ""
	continued := false
	for _, line := range strings.Split(text, "\n") {
		if continued {
			line = strings.TrimLeft(line, " \t\f")
		}
		logical += line
		slashes := 0
		for i := len(logical) - 1; i >= 0 && logical[i] == '\\'; i-- {
			slashes++
		}
		if slashes%2 == 1 {
			logical = logical[:len(logical)-1]
			continued = true
			continue
		}
		continued = false
		line = strings.TrimLeft(logical, " \t\f")
		logical = ""
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		boundary := len(line)
		escaped := false
		for i, b := range []byte(line) {
			if !escaped && strings.ContainsRune("=: \t\f", rune(b)) {
				boundary = i
				break
			}
			if b == '\\' {
				escaped = !escaped
			} else {
				escaped = false
			}
		}
		value := strings.TrimLeft(line[boundary:], " \t\f")
		if len(value) > 0 && (value[0] == '=' || value[0] == ':') {
			value = strings.TrimLeft(value[1:], " \t\f")
		}
		key, err := unescape(line[:boundary])
		if err != nil {
			return nil, err
		}
		decoded, err := unescape(value)
		if err != nil {
			return nil, err
		}
		properties[key] = decoded
	}
	if continued {
		return nil, errors.New("incomplete properties continuation")
	}
	return properties, nil
}
func unescape(value string) (string, error) {
	var units []uint16
	for i := 0; i < len(value); {
		if value[i] != '\\' {
			// Input was expanded from ISO bytes to UTF-8 before parsing.
			r := []rune(value[i:])[0]
			encoded := string(r)
			units = append(units, utf16.Encode([]rune{r})...)
			i += len(encoded)
			continue
		}
		i++
		if i == len(value) {
			break
		}
		switch value[i] {
		case 'u':
			if i+5 > len(value) {
				return "", errors.New("invalid properties Unicode escape")
			}
			number, err := strconv.ParseUint(value[i+1:i+5], 16, 16)
			if err != nil {
				return "", errors.New("invalid properties Unicode escape")
			}
			units = append(units, uint16(number))
			i += 5
		case 't', 'n', 'r', 'f':
			units = append(units, map[byte]uint16{'t': '\t', 'n': '\n', 'r': '\r', 'f': '\f'}[value[i]])
			i++
		default:
			r := []rune(value[i:])[0]
			units = append(units, utf16.Encode([]rune{r})...)
			i += len(string(r))
		}
	}
	for i, u := range units {
		if u >= 0xd800 && u <= 0xdbff && (i+1 == len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff) {
			return "", fmt.Errorf("invalid properties surrogate")
		}
	}
	return string(utf16.Decode(units)), nil
}
