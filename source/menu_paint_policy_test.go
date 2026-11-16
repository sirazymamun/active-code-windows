package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Validate painted geometry, rather than treating every fill call as a tile.
// A 1–2 px focus underline is allowed; filled menu backgrounds remain forbidden.
func checkMenuPaint(source string) error {
	start := strings.Index(source, "for i, t := range titles {")
	if start < 0 {
		return fmt.Errorf("menu draw loop missing")
	}
	end := strings.Index(source[start:], "gw, cw, top, bottom :=")
	if end < 0 {
		return fmt.Errorf("menu draw loop end missing")
	}
	tree, err := parser.ParseFile(token.NewFileSet(), "menu.go", "package main\nfunc paint(){\n"+source[start:start+end]+"\n}", 0)
	if err != nil {
		return err
	}
	ast.Inspect(tree, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if name.Name == "roundedFill" {
			err = fmt.Errorf("rounded menu background returned")
			return false
		}
		if name.Name != "fill" {
			return true
		}
		if len(call.Args) != 3 {
			err = fmt.Errorf("unexpected menu fill")
			return false
		}
		rect, ok := call.Args[1].(*ast.CompositeLit)
		if !ok || len(rect.Elts) != 4 {
			err = fmt.Errorf("menu background is not a thin underline")
			return false
		}
		height, ok := rect.Elts[3].(*ast.BasicLit)
		if !ok {
			err = fmt.Errorf("unbounded menu background height")
			return false
		}
		value, conversion := strconv.Atoi(height.Value)
		if conversion != nil || value < 1 || value > 2 {
			err = fmt.Errorf("menu background height %s exceeds focus underline", height.Value)
		}
		return true
	})
	return err
}

func TestMenuPaintAllowsThinFocusUnderlineWithoutBackgroundTiles(t *testing.T) {
	source, err := os.ReadFile("native_ui_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if err = checkMenuPaint(string(source)); err != nil {
		t.Fatal(err)
	}
}

func TestMenuPaintStillRejectsReturnedBackgroundTiles(t *testing.T) {
	for _, call := range []string{
		"fill(hdc, rectF{r.x,r.y,r.w,r.h}, cyan)",
		"fill(hdc, rectF{r.x,r.y,r.w,30}, cyan)",
		"roundedFill(hdc, r, 8, cyan)",
	} {
		source := "for i, t := range titles { " + call + " }; gw, cw, top, bottom := g.bounds()"
		if checkMenuPaint(source) == nil {
			t.Fatalf("background tile accepted: %s", call)
		}
	}
}
