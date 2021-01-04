// Package benchstatter is used to run benchstatter programmatically
package benchstatter

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/monochromegane/mdt"
	"golang.org/x/perf/benchstat"
)

// Benchstat is a benchstat runner
type Benchstat struct {
	// DeltaTest is the test to use to decide if a change is significant.
	// If nil, it defaults to UTest.
	DeltaTest benchstat.DeltaTest

	// Alpha is the p-value cutoff to report a change as significant.
	// If zero, it defaults to 0.05.
	Alpha float64

	// AddGeoMean specifies whether to add a line to the table
	// showing the geometric mean of all the benchmark results.
	AddGeoMean bool

	// SplitBy specifies the labels to split results by.
	// By default, results will only be split by full name.
	SplitBy []string

	// Order specifies the row display order for this table.
	// If Order is nil, the table rows are printed in order of
	// first appearance in the input.
	Order benchstat.Order

	// ReverseOrder reverses the display order. Not valid if Order is nil.
	ReverseOrder bool

	// OutputFormatter determines how the output will be formatted. Default is TextFormatter
	OutputFormatter OutputFormatter
}

// OutputFormatter formats benchstat output
type OutputFormatter func(w io.Writer, tables []*benchstat.Table) error

// Collection returns a *benchstat.Collection
func (b *Benchstat) Collection() *benchstat.Collection {
	order := b.Order
	if b.ReverseOrder {
		order = benchstat.Reverse(order)
	}

	return &benchstat.Collection{
		Alpha:      b.Alpha,
		AddGeoMean: b.AddGeoMean,
		DeltaTest:  b.DeltaTest,
		SplitBy:    b.SplitBy,
		Order:      order,
	}
}

// Run runs benchstat
func (b *Benchstat) Run(files ...string) (*benchstat.Collection, error) {
	collection := b.Collection()
	err := AddCollectionFiles(collection, files...)
	if err != nil {
		return nil, err
	}
	return collection, nil
}

// OutputTables outputs the results from tables using b.OutputFormatter
func (b *Benchstat) OutputTables(writer io.Writer, tables []*benchstat.Table) error {
	formatter := b.OutputFormatter
	if formatter == nil {
		formatter = TextFormatter(nil)
	}
	return formatter(writer, tables)
}

// AddCollectionFiles adds files to a collection
func AddCollectionFiles(c *benchstat.Collection, files ...string) error {
	for _, file := range files {
		f, err := os.Open(file) //nolint:gosec // this is fine
		if err != nil {
			return err
		}
		err = c.AddFile(file, f)
		if err != nil {
			return err
		}
		err = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// TextFormatterOptions options for a text OutputFormatter
type TextFormatterOptions struct{}

// TextFormatter returns a text OutputFormatter
func TextFormatter(_ *TextFormatterOptions) OutputFormatter {
	return func(w io.Writer, tables []*benchstat.Table) error {
		benchstat.FormatText(w, tables)
		return nil
	}
}

// CSVFormatterOptions options for a csv OutputFormatter
type CSVFormatterOptions struct {
	NoRange bool
}

// CSVFormatter returns a csv OutputFormatter
func CSVFormatter(opts *CSVFormatterOptions) OutputFormatter {
	noRange := false
	if opts != nil {
		noRange = opts.NoRange
	}
	return func(w io.Writer, tables []*benchstat.Table) error {
		benchstat.FormatCSV(w, tables, noRange)
		return nil
	}
}

func csv2Markdown(data []byte) ([]string, error) {
	var csvTables [][]byte
	var currentTable []byte
	var err error
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			if len(currentTable) > 0 {
				csvTables = append(csvTables, currentTable)
			}
			currentTable = []byte{}
			continue
		}
		line = append(line, '\n')
		currentTable = append(currentTable, line...)
	}
	err = scanner.Err()
	if err != nil {
		return nil, err
	}
	if len(currentTable) > 0 {
		csvTables = append(csvTables, currentTable)
	}
	var mdTables []string
	for _, csvTable := range csvTables {
		var buf bytes.Buffer
		err = reFloatCsv(&buf, bytes.NewReader(csvTable))
		if err != nil {
			return nil, err
		}
		var mdTable string
		mdTable, err = mdt.Convert("", &buf)
		if err != nil {
			return nil, err
		}
		mdTables = append(mdTables, mdTable)
	}
	return mdTables, nil
}

// MarkdownFormatterOptions options for a markdown OutputFormatter
type MarkdownFormatterOptions struct {
	CSVFormatterOptions
}

func reFloatCsv(dest io.Writer, src io.Reader) error {
	csvSrc := csv.NewReader(src)
	csvSrc.FieldsPerRecord = -1
	csvDest := csv.NewWriter(dest)
	var err error
	var row []string
	for {
		row, err = csvSrc.Read()
		if err != nil {
			break
		}
		for i, val := range row {
			f, fErr := strconv.ParseFloat(val, 64)
			if fErr != nil {
				continue
			}
			row[i] = strconv.FormatFloat(f, 'f', -1, 64)
		}
		err = csvDest.Write(row)
		if err != nil {
			break
		}
	}
	if err != io.EOF {
		return err
	}

	csvDest.Flush()
	return csvDest.Error()
}

// MarkdownFormatter return a markdown OutputFormatter
func MarkdownFormatter(opts *MarkdownFormatterOptions) OutputFormatter {
	return func(w io.Writer, tables []*benchstat.Table) error {
		if opts == nil {
			opts = new(MarkdownFormatterOptions)
		}
		csvFormatter := CSVFormatter(&opts.CSVFormatterOptions)
		var buf bytes.Buffer
		err := csvFormatter(&buf, tables)
		if err != nil {
			return err
		}
		mdTables, err := csv2Markdown(buf.Bytes())
		if err != nil {
			return err
		}
		output := strings.Join(mdTables, "\n")
		_, err = w.Write([]byte(output))
		if err != nil {
			return err
		}
		return nil
	}
}

// HTMLFormatterOptions options for an html OutputFormatter
type HTMLFormatterOptions struct {
	Header string
	Footer string
}

// HTMLFormatter return an html OutputFormatter
func HTMLFormatter(opts *HTMLFormatterOptions) OutputFormatter {
	header := defaultHTMLHeader
	footer := defaultHTMLFooter
	if opts != nil {
		header = opts.Header
		footer = opts.Footer
	}
	return func(w io.Writer, tables []*benchstat.Table) error {
		if header != "" {
			_, err := w.Write([]byte(header))
			if err != nil {
				return err
			}
		}
		var buf bytes.Buffer
		benchstat.FormatHTML(&buf, tables)
		_, err := w.Write(buf.Bytes())
		if err != nil {
			return err
		}
		if footer != "" {
			_, err = w.Write([]byte(footer))
			if err != nil {
				return err
			}
		}
		return nil
	}
}

var defaultHTMLHeader = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Performance Result Comparison</title>
<style>
.benchstat { border-collapse: collapse; }
.benchstat th:nth-child(1) { text-align: left; }
.benchstat tbody td:nth-child(1n+2):not(.note) { text-align: right; padding: 0em 1em; }
.benchstat tr:not(.configs) th { border-top: 1px solid #666; border-bottom: 1px solid #ccc; }
.benchstat .nodelta { text-align: center !important; }
.benchstat .better td.delta { font-weight: bold; }
.benchstat .worse td.delta { font-weight: bold; color: #c00; }
</style>
</head>
<body>
`

var defaultHTMLFooter = `</body>
</html>
`
