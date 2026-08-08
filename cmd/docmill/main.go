// Command docmill converts documents to Markdown.
//
//	docmill <input.pdf>          convert a PDF to Markdown (native backend)
//	docmill convert <input.pdf>  same as above, explicit
//	docmill convert -learned-layout <input.pdf>
//	                             convert with the learned layout classifier
//	docmill convert -region-markdown <input.pdf>
//	                             the learned region stage drives the whole page
//	docmill forms export <input.pdf>
//	docmill forms layout <input.pdf>
//	docmill forms fill <input.pdf> <output.pdf> [values.json]
//	docmill json                 read a JSON document on stdin, write Markdown
//	docmill render <input.pdf>   draw every object the page draws as a box, to PNG
//	docmill benchmark            run the cross-tool DPBench benchmark
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ivanvanderbyl/docmill/v2/pkg/telemetry"
)

func main() {
	ctx := context.Background()
	flush := setupTelemetry(ctx, os.Args)
	code := dispatch(ctx, os.Args, os.Stdin, os.Stdout, os.Stderr)
	flush()
	os.Exit(code)
}

// setupTelemetry installs OTLP exporters when telemetry is enabled (DOCMILL_OTEL
// or an OTEL endpoint is set) and returns a flush function to call before exit.
func setupTelemetry(ctx context.Context, args []string) func() {
	noop := func() {}
	if !telemetry.Enabled() {
		return noop
	}
	shutdown, err := telemetry.Setup(ctx, telemetry.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "telemetry setup: %v\n", err)
		return noop
	}
	return func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(flushCtx)
	}
}

func dispatch(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		usage(stderr, args)
		return 2
	}

	switch args[1] {
	case "benchmark":
		return runBenchmark(ctx, args[2:], stdout, stderr)
	case "json":
		if err := runJSON(stdin, stdout, stderr); err != nil {
			return 1
		}
		return 0
	case "convert":
		if err := runConvert(ctx, args[2:], stdout, stderr); err != nil {
			return 1
		}
		return 0
	case "forms":
		if err := runForms(ctx, args[2:], stdin, stdout, stderr); err != nil {
			return 1
		}
		return 0
	case "render":
		if err := runRender(ctx, args[2:], stdout, stderr); err != nil {
			return 1
		}
		return 0
	case "help", "-h", "--help":
		usage(stdout, args)
		return 0
	default:
		// Bare form: treat the operand as a PDF path -> Markdown.
		if err := runConvert(ctx, args[1:], stdout, stderr); err != nil {
			return 1
		}
		return 0
	}
}

func usage(w io.Writer, args []string) {
	name := "docmill"
	if len(args) > 0 {
		name = filepath.Base(args[0])
	}
	_, _ = fmt.Fprintf(w, `usage: %s <command> [arguments]

Commands:
  <input.pdf>          convert a PDF to Markdown (default, native backend)
  convert <input.pdf>  convert a PDF to Markdown
                       -learned-layout classifies lines with the embedded layout
                       model (headings, list items, figure innards, formulas)
                       instead of the hand-tuned detectors
  forms export <input.pdf>
                       write AcroForm field values as JSON
  forms layout <input.pdf>
                       write AcroForm field bounding boxes and labels as JSON
                       (per page, top-left-origin points, unfilled fields included)
  forms fill <input.pdf> <output.pdf> [values.json]
                       read AcroForm field JSON from a file or stdin and write a filled PDF
  render <input.pdf>   write one PNG per page outlining every object the
                       content-stream interpreter says the page draws — text,
                       paths, images, shadings and form XObjects — each clipped
                       to what is actually visible
                       -out dir, -scale n, -pages 1,2, -kinds image,path
                       -regions draws the learned region stage instead: one
                       labelled, colour-coded box per detected region
                       (-json emits the same decomposition as JSON)
  json                 read a JSON document on stdin, write Markdown to stdout
  benchmark            run the cross-tool DPBench benchmark
`, name)
}
