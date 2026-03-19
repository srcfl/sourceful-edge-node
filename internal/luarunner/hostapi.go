package luarunner

import (
	"log"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// ToFloat64 extracts a float64 from a Lua return value.
func ToFloat64(v interface{}) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case lua.LNumber:
		return float64(n), true
	case *lua.LNilType:
		return 0, false
	default:
		return 0, false
	}
}

// EmitFunc is the callback signature for host.emit().
type EmitFunc func(derType string, data map[string]float64)

// LogFunc is an optional callback for host.log(). If nil, log.Printf is used.
type LogFunc func(msg string)

// RegisterStandardHostAPI registers the standard host functions on the runtime.
func RegisterStandardHostAPI(rt *Runtime, emitFn EmitFunc, logFn LogFunc) {
	rt.SetUserData("emit_fn", emitFn)
	if logFn != nil {
		rt.SetUserData("log_fn", logFn)
	}

	rt.RegisterHostFunc("log", makeHostLog(rt))
	rt.RegisterHostFunc("millis", hostMillis)
	rt.RegisterHostFunc("emit", makeHostEmit(rt))
	rt.RegisterHostFunc("set_make", makeSetMake(rt))
}

func makeSetMake(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		name := L.CheckString(1)
		rt.SetUserData("driver_make", name)
		return 0
	}
}

func makeHostLog(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		msg := L.CheckString(1)
		logRaw, ok := rt.GetUserData("log_fn")
		if ok {
			if logFn, ok := logRaw.(LogFunc); ok {
				logFn(msg)
				return 0
			}
		}
		log.Printf("[lua] %s", msg)
		return 0
	}
}

func hostMillis(L *lua.LState) int {
	ms := time.Now().UnixMilli()
	L.Push(lua.LNumber(ms))
	return 1
}

func makeHostEmit(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		derType := L.CheckString(1)
		tbl := L.CheckTable(2)

		data := make(map[string]float64)
		tbl.ForEach(func(key, value lua.LValue) {
			if k, ok := key.(lua.LString); ok {
				if v, ok := value.(lua.LNumber); ok {
					data[string(k)] = float64(v)
				}
			}
		})

		emitRaw, ok := rt.GetUserData("emit_fn")
		if !ok {
			L.Push(lua.LFalse)
			return 1
		}

		emitFn, ok := emitRaw.(EmitFunc)
		if !ok {
			L.Push(lua.LFalse)
			return 1
		}

		emitFn(derType, data)
		L.Push(lua.LTrue)
		return 1
	}
}
