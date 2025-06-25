package main

import (
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kong"
)

var logwriter io.Writer

func log(f string, args ...any) {
	fmt.Fprintf(logwriter, f, args...)
}

var CLI struct {
	Push PushCmd `cmd:"" help:"Push local commits to the remote."`
}

func main() {
	logwriter = os.Stderr

	ctx := kong.Parse(&CLI)
	ctx.FatalIfErrorf(ctx.Run())
}
