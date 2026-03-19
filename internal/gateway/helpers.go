package gateway

import (
	"runtime"

	lua "github.com/yuin/gopher-lua"
)

func luaString(s string) lua.LValue {
	return lua.LString(s)
}

func luaNumber(n int) lua.LValue {
	return lua.LNumber(n)
}

func runtimeGOOS() string {
	return runtime.GOOS
}

func runtimeGOARCH() string {
	return runtime.GOARCH
}
