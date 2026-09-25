// Command molangcheck parses Molang strings handed to it on stdin and reports
// the ones molang-go refuses.
//
// Input is one JSON object per line:
//
//	{"file": "...", "pointer": "/a/b", "kind": "general", "text": "q.foo"}
//
// Output is one JSON object per input line that failed to parse, with the
// input fields plus "error". A final summary line {"summary": {...}} carries
// the counts. The exit status is 0 whether or not anything failed; the caller
// decides what a failure means.
//
// It is the parsing half of apps/vscode/tools/check-molang-paths.mjs and is
// run from the repository root:
//
//	go run ./apps/vscode/tools/molangcheck
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	molang "github.com/stirante/molang-go"
)

type item struct {
	File    string `json:"file"`
	Pointer string `json:"pointer"`
	Kind    string `json:"kind"`
	Text    string `json:"text"`
}

type failure struct {
	item
	Error string `json:"error"`
}

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<26)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	total, failed := 0, 0
	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var it item
		if err := json.Unmarshal(line, &it); err != nil {
			fmt.Fprintf(os.Stderr, "molangcheck: bad input line: %v\n", err)
			os.Exit(2)
		}
		total++
		if _, err := molang.Parse(it.Text); err != nil {
			failed++
			_ = enc.Encode(failure{item: it, Error: err.Error()})
		}
	}
	if err := in.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "molangcheck: %v\n", err)
		os.Exit(2)
	}
	_ = enc.Encode(map[string]any{"summary": map[string]int{"parsed": total, "failed": failed}})
}
