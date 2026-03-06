// Command jolt is a streaming JSONL plumbing tool.
// It reads JSONL from stdin, applies transformations via flags,
// and writes JSONL to stdout.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

func main() {
	pick := flag.String("pick", "", "comma-separated fields to extract")
	filter := flag.String("filter", "", "field=value filter (keep matching lines)")
	merge := flag.String("merge", "", "JSON object to merge into each line")
	count := flag.Bool("count", false, "count lines and output {\"count\": N}")
	head := flag.Int("head", 0, "take first N lines")
	validate := flag.Bool("validate", false, "skip invalid JSON lines")
	flag.Parse()

	// Parse merge data upfront
	var mergeData map[string]interface{}
	if *merge != "" {
		if err := json.Unmarshal([]byte(*merge), &mergeData); err != nil {
			jsonutil.Fatal(fmt.Sprintf("invalid -merge JSON: %v", err))
		}
	}

	// Parse filter upfront
	var filterKey, filterVal string
	if *filter != "" {
		parts := strings.SplitN(*filter, "=", 2)
		if len(parts) != 2 {
			jsonutil.Fatal("invalid -filter format, use field=value")
		}
		filterKey = parts[0]
		filterVal = parts[1]
	}

	// Parse pick fields
	var pickFields []string
	if *pick != "" {
		pickFields = strings.Split(*pick, ",")
		for i, f := range pickFields {
			pickFields[i] = strings.TrimSpace(f)
		}
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	lineCount := 0
	taken := 0

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse JSON
		var obj map[string]interface{}
		if err := json.Unmarshal(line, &obj); err != nil {
			if *validate {
				continue // skip invalid lines
			}
			// Not validating — pass through raw
			if !*count {
				os.Stdout.Write(line)
				os.Stdout.Write([]byte("\n"))
			}
			lineCount++
			taken++
			if *head > 0 && taken >= *head {
				break
			}
			continue
		}

		// Apply filter
		if filterKey != "" {
			val, ok := obj[filterKey]
			if !ok {
				continue
			}
			if !matchValue(val, filterVal) {
				continue
			}
		}

		// Apply merge
		if mergeData != nil {
			for k, v := range mergeData {
				obj[k] = v
			}
		}

		// Apply pick
		if len(pickFields) > 0 {
			picked := make(map[string]interface{})
			for _, f := range pickFields {
				if v, ok := obj[f]; ok {
					picked[f] = v
				}
			}
			obj = picked
		}

		lineCount++

		// Head limit
		if *head > 0 {
			taken++
			if taken > *head {
				break
			}
		}

		// Output (unless counting)
		if !*count {
			out, err := json.Marshal(obj)
			if err != nil {
				continue
			}
			os.Stdout.Write(out)
			os.Stdout.Write([]byte("\n"))
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, `{"error": "scan: %s"}`+"\n", err.Error())
	}

	// Output count if requested
	if *count {
		fmt.Fprintf(os.Stdout, `{"count":%d}`+"\n", lineCount)
	}
}

// matchValue compares an interface value against a string target.
func matchValue(val interface{}, target string) bool {
	switch v := val.(type) {
	case string:
		return v == target
	case float64:
		f, err := strconv.ParseFloat(target, 64)
		if err != nil {
			return false
		}
		return v == f
	case bool:
		b, err := strconv.ParseBool(target)
		if err != nil {
			return false
		}
		return v == b
	case nil:
		return target == "null" || target == ""
	default:
		// Marshal and compare
		j, err := json.Marshal(v)
		if err != nil {
			return false
		}
		return string(j) == target
	}
}
