package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/internal/releaseversion"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("release-version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	current := flags.String("current", "", "latest stable release tag")
	labelList := flags.String("labels", "", "comma-separated pull request labels")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	var labels []string
	if *labelList != "" {
		labels = strings.Split(*labelList, ",")
	}
	next, err := releaseversion.Next(*current, labels)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, next)
	return 0
}
