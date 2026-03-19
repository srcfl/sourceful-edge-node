package luarunner

import (
	"encoding/binary"
	"encoding/json"
	"math"

	lua "github.com/yuin/gopher-lua"
)

// RegisterDecodeHostFuncs adds register decoding helpers to the runtime.
func RegisterDecodeHostFuncs(rt *Runtime) {
	rt.RegisterHostFunc("decode_i16", decodeI16)
	rt.RegisterHostFunc("decode_u32", decodeU32)
	rt.RegisterHostFunc("decode_i32", decodeI32)
	rt.RegisterHostFunc("decode_u32_le", decodeU32LE)
	rt.RegisterHostFunc("decode_i32_le", decodeI32LE)
	rt.RegisterHostFunc("decode_f32", decodeF32)
	rt.RegisterHostFunc("decode_u64", decodeU64)
	rt.RegisterHostFunc("scale", scaleSF)
	rt.RegisterHostFunc("json_decode", jsonDecode)
}

func decodeI16(L *lua.LState) int {
	val := uint16(L.CheckNumber(1))
	L.Push(lua.LNumber(int16(val)))
	return 1
}

func decodeU32(L *lua.LState) int {
	hi := uint16(L.CheckNumber(1))
	lo := uint16(L.CheckNumber(2))
	result := uint32(hi)<<16 | uint32(lo)
	L.Push(lua.LNumber(result))
	return 1
}

func decodeI32(L *lua.LState) int {
	hi := uint16(L.CheckNumber(1))
	lo := uint16(L.CheckNumber(2))
	result := int32(uint32(hi)<<16 | uint32(lo))
	L.Push(lua.LNumber(result))
	return 1
}

func decodeU32LE(L *lua.LState) int {
	lo := uint16(L.CheckNumber(1))
	hi := uint16(L.CheckNumber(2))
	result := uint32(hi)<<16 | uint32(lo)
	L.Push(lua.LNumber(result))
	return 1
}

func decodeI32LE(L *lua.LState) int {
	lo := uint16(L.CheckNumber(1))
	hi := uint16(L.CheckNumber(2))
	result := int32(uint32(hi)<<16 | uint32(lo))
	L.Push(lua.LNumber(result))
	return 1
}

func decodeF32(L *lua.LState) int {
	hi := uint16(L.CheckNumber(1))
	lo := uint16(L.CheckNumber(2))
	var buf [4]byte
	binary.BigEndian.PutUint16(buf[0:2], hi)
	binary.BigEndian.PutUint16(buf[2:4], lo)
	bits := binary.BigEndian.Uint32(buf[:])
	f := math.Float32frombits(bits)
	L.Push(lua.LNumber(f))
	return 1
}

func decodeU64(L *lua.LState) int {
	w1 := uint16(L.CheckNumber(1))
	w2 := uint16(L.CheckNumber(2))
	w3 := uint16(L.CheckNumber(3))
	w4 := uint16(L.CheckNumber(4))
	result := uint64(w1)<<48 | uint64(w2)<<32 | uint64(w3)<<16 | uint64(w4)
	L.Push(lua.LNumber(result))
	return 1
}

func scaleSF(L *lua.LState) int {
	value := float64(L.CheckNumber(1))
	sf := float64(L.CheckNumber(2))
	if math.Abs(sf) > 10 {
		sf = 0
	}
	L.Push(lua.LNumber(value * math.Pow(10, sf)))
	return 1
}

func jsonDecode(L *lua.LState) int {
	str := L.CheckString(1)
	var data interface{}
	if err := json.Unmarshal([]byte(str), &data); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(goToLua(L, data))
	return 1
}

func goToLua(L *lua.LState, v interface{}) lua.LValue {
	switch val := v.(type) {
	case nil:
		return lua.LNil
	case bool:
		if val {
			return lua.LTrue
		}
		return lua.LFalse
	case float64:
		return lua.LNumber(val)
	case string:
		return lua.LString(val)
	case []interface{}:
		tbl := L.NewTable()
		for i, item := range val {
			tbl.RawSetInt(i+1, goToLua(L, item))
		}
		return tbl
	case map[string]interface{}:
		tbl := L.NewTable()
		for k, item := range val {
			tbl.RawSetString(k, goToLua(L, item))
		}
		return tbl
	default:
		return lua.LNil
	}
}
