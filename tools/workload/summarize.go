package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
)

// summarize prints, for every recipe and operation in raw result files, the
// sample count, the median, the 95th percentile when there are at least 20
// samples, the slowest sample, the largest peak memory, and the median
// allocation. The raw samples stay the record; this is a reading aid.
func summarize(args []string) error {
	if len(args) == 0 {
		return errors.New("summarize needs result files")
	}

	type key struct{ recipe, operation string }
	type values struct {
		seconds        []float64
		peakKiB, floor int64
		allocs, bytes  []float64
	}
	groups := map[key]*values{}
	var order []key

	for _, path := range args {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for scanner.Scan() {
			var s struct {
				Recipe, Operation string
				Seconds           float64
				PeakKiB           int64   `json:"peak_kib"`
				FloorKiB          int64   `json:"floor_kib"`
				AllocBytes        float64 `json:"alloc_bytes"`
				Allocs            float64 `json:"allocs"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &s); err != nil || s.Operation == "" {
				continue // environment records and anything else
			}
			k := key{s.Recipe, s.Operation}
			v, ok := groups[k]
			if !ok {
				v = &values{}
				groups[k] = v
				order = append(order, k)
			}
			v.seconds = append(v.seconds, s.Seconds)
			v.peakKiB = max(v.peakKiB, s.PeakKiB)
			v.floor = max(v.floor, s.FloorKiB)
			v.allocs = append(v.allocs, s.Allocs)
			v.bytes = append(v.bytes, s.AllocBytes)
		}
		_ = f.Close()
		if err := scanner.Err(); err != nil {
			return err
		}
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	fmt.Fprintln(w, "| Workload | Operation | n | Median | p95 | Slowest | Peak memory | Allocated (median) |")
	fmt.Fprintln(w, "| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, k := range order {
		v := groups[k]
		slices.Sort(v.seconds)
		p95 := "n/a"
		if len(v.seconds) >= 20 {
			p95 = duration(nearestRank(v.seconds, 0.95))
		}
		peak := ""
		switch {
		case v.peakKiB > 0 && v.peakKiB <= v.floor:
			peak = fmt.Sprintf("at most %.0f MiB", float64(v.floor)/1024)
		case v.peakKiB > 0:
			peak = fmt.Sprintf("%.0f MiB", float64(v.peakKiB)/1024)
		}
		allocated := ""
		if m := median(v.bytes); m > 0 {
			allocated = fmt.Sprintf("%s in %.0f allocations", bytesLabel(m), median(v.allocs))
		}
		row := []string{k.recipe, k.operation, fmt.Sprint(len(v.seconds)), duration(median(v.seconds)), p95, duration(v.seconds[len(v.seconds)-1]), peak, allocated}
		switch {
		case strings.HasPrefix(k.operation, "heap"):
			row = []string{k.recipe, k.operation, fmt.Sprint(len(v.seconds)), "", "", "", bytesLabel(median(v.bytes)) + " live", ""}
		case v.seconds[len(v.seconds)-1] == 0:
			// A memory-only record carries no duration.
			row[3], row[4], row[5] = "", "", ""
		}
		fmt.Fprintln(w, "| "+strings.Join(row, " | ")+" |")
	}
	return nil
}

// nearestRank is the smallest sample with at least p of the samples at or
// below it. sorted must be sorted.
func nearestRank(sorted []float64, p float64) float64 {
	return sorted[int(math.Ceil(p*float64(len(sorted))))-1]
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(values))
	return sorted[(len(sorted)-1)/2]
}

func duration(seconds float64) string {
	switch {
	case seconds >= 1:
		return fmt.Sprintf("%.2f s", seconds)
	case seconds >= 0.001:
		return fmt.Sprintf("%.1f ms", seconds*1e3)
	default:
		return fmt.Sprintf("%.0f µs", seconds*1e6)
	}
}

func bytesLabel(b float64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KiB", b/(1<<10))
	default:
		return fmt.Sprintf("%.0f B", b)
	}
}
