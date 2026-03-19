package modbus

import (
	"fmt"
	"math"
)

// DataType represents the type of a Modbus register
type DataType int

const (
	U16 DataType = iota
	I16
	U32
	I32
	F32 // IEEE 754 single-precision float
	STR // Multi-word ASCII string
	U64 // Unsigned 64-bit integer (4 words)
)

func (d DataType) String() string {
	return [...]string{"U16", "I16", "U32", "I32", "F32", "STR", "U64"}[d]
}

// ParseDataType converts a string to DataType.
func ParseDataType(s string) (DataType, error) {
	switch s {
	case "U16":
		return U16, nil
	case "I16":
		return I16, nil
	case "U32":
		return U32, nil
	case "I32":
		return I32, nil
	case "F32":
		return F32, nil
	case "STR":
		return STR, nil
	case "U64":
		return U64, nil
	default:
		return 0, fmt.Errorf("unknown data type: %s", s)
	}
}

// WordCount returns the number of Modbus registers needed for a data type.
func (d DataType) WordCount() int {
	switch d {
	case U16, I16:
		return 1
	case U32, I32, F32:
		return 2
	case U64:
		return 4
	default:
		return 1
	}
}

// Endianness for multi-word registers
type Endianness int

const (
	Big Endianness = iota
	Little
)

func (e Endianness) String() string {
	return [...]string{"BIG", "LITTLE"}[e]
}

// RegisterDef defines a Modbus register
type RegisterDef struct {
	Address      uint16
	Name         string
	SemanticName string // Standardized name for device-agnostic access
	Description  string
	Unit         string
	Category     string // pv, battery, meter, grid, control
	DataType     DataType
	Scale        float64
	Words        int
	Endianness   Endianness
	UseHolding   bool
}

// DecodeValue decodes register words to a float64 value according to the register definition
func DecodeValue(registers []uint16, def *RegisterDef) (float64, error) {
	if len(registers) < def.Words {
		return 0, fmt.Errorf("expected %d registers, got %d", def.Words, len(registers))
	}

	var raw int64
	switch def.DataType {
	case U16:
		raw = int64(registers[0])
	case I16:
		raw = int64(int16(registers[0]))
	case U32:
		raw = int64(decodeU32(registers, def.Endianness))
	case I32:
		raw = int64(int32(decodeU32(registers, def.Endianness)))
	case F32:
		bits := decodeU32(registers, def.Endianness)
		return float64(math.Float32frombits(bits)), nil
	case U64:
		raw = int64(decodeU64(registers, def.Endianness))
	case STR:
		return 0, fmt.Errorf("use DecodeString for STR registers")
	}

	if def.Scale != 0 {
		return float64(raw) * def.Scale, nil
	}
	return float64(raw), nil
}

// DecodeRawValue decodes register words without applying scale factor.
func DecodeRawValue(registers []uint16, dataType DataType, endian Endianness) (float64, error) {
	def := &RegisterDef{DataType: dataType, Words: dataType.WordCount(), Endianness: endian, Scale: 1.0}
	return DecodeValue(registers, def)
}

func decodeU32(registers []uint16, endian Endianness) uint32 {
	if endian == Big {
		return (uint32(registers[0]) << 16) | uint32(registers[1])
	}
	return (uint32(registers[1]) << 16) | uint32(registers[0])
}

func decodeU64(registers []uint16, endian Endianness) uint64 {
	if endian == Big {
		return (uint64(registers[0]) << 48) | (uint64(registers[1]) << 32) | (uint64(registers[2]) << 16) | uint64(registers[3])
	}
	return (uint64(registers[3]) << 48) | (uint64(registers[2]) << 32) | (uint64(registers[1]) << 16) | uint64(registers[0])
}

// DecodeString decodes register words into a string (2 bytes per register, big-endian).
func DecodeString(registers []uint16) string {
	bytes := make([]byte, 0, len(registers)*2)
	for _, reg := range registers {
		bytes = append(bytes, byte(reg>>8), byte(reg&0xFF))
	}
	for i, b := range bytes {
		if b == 0 {
			bytes = bytes[:i]
			break
		}
	}
	return string(bytes)
}

// BytesToRegisters converts a byte slice (from Modbus response) to uint16 registers.
func BytesToRegisters(data []byte) []uint16 {
	count := len(data) / 2
	regs := make([]uint16, count)
	for i := 0; i < count; i++ {
		regs[i] = uint16(data[i*2])<<8 | uint16(data[i*2+1])
	}
	return regs
}
