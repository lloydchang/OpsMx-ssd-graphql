package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"regexp"
	"strings"
)

const (
	buffersize          = 256 * 1024
	authHeader          = "X-Dgraph-AuthToken"
	dgraphAuthorization = "# Dgraph.Authorization"
)

var (
	filenameFlag   = flag.String("in", "-", "input filename, with '-' for stdin")
	wipeFlag       = flag.Bool("wipe", false, "erase all data and schema before uploading")
	authtokenFlag  = flag.String("authtoken", "", "authentication token for dgraph")
	dgraphauthFlag = flag.String("dgraphauth", "", "Dgraph authorization object to be passed onto the schema")
	urlFlag        = flag.String("url", "", "URL for the GraphQL endpoint")
	dumpFlag       = flag.Bool("dump", false, "also output processed schema to stdout")
	dryFlag        = flag.Bool("dry", false, "don't submit anything, just process the input")

	mimeType = mime.FormatMediaType("application/json", map[string]string{"charset": "utf-8"})
)

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	flag.Parse()

	ctx := context.Background()

	if *urlFlag == "" && !*dryFlag {
		log.Printf("Must specify a URL")
		flag.Usage()
		os.Exit(1)
	}

	infile := os.Stdin
	if *filenameFlag != "-" {
		var err error
		if infile, err = os.Open(*filenameFlag); err != nil {
			fmt.Printf("Error opening file: %v", err)
			os.Exit(1)
		}
		defer infile.Close()
	}

	if *dgraphauthFlag == "" {
		log.Printf("dgraphauth flag has not been set")
		os.Exit(1)
	}

	rp := regexp.MustCompile("^\\s*@opsmxAuthRule\\(([^\\)]+)\\)\\s*$")

	scanner := bufio.NewScanner(infile)
	scannerBuffer := make([]byte, buffersize)
	scanner.Buffer(scannerBuffer, buffersize)

	outbuf := &bytes.Buffer{}

	for scanner.Scan() {
		processLine(rp, outbuf, scanner.Text())
	}
	check(scanner.Err())

	processAuthObject(outbuf, *dgraphauthFlag)

	schemaBytes := outbuf.Bytes()
	if *dumpFlag {
		os.Stdout.Write(schemaBytes)
	}

	if *dryFlag {
		os.Exit(0)
	}

	if *wipeFlag {
		if err := wipe(ctx, *urlFlag, *authtokenFlag); err != nil {
			log.Printf("cannot wipe: %v", err)
			os.Exit(1)
		}
	}

	submitSchema(ctx, *urlFlag, *authtokenFlag, schemaBytes)
}

func processLine(rp *regexp.Regexp, out io.Writer, t string) {
	match := rp.FindStringSubmatch(t)
	if len(match) == 0 {
		fmt.Fprintf(out, "%s\n", t)
		return
	}

	process(out, strings.TrimSpace(match[1]))
}

func processAuthObject(out io.Writer, t string) {
	fmt.Fprintf(out, "\n%s %s\n", dgraphAuthorization, t)
}

func process(out io.Writer, t string) {
	directives := map[string]string{
		"path": "",
	}
	items := strings.Split(t, " ")
	for _, i := range items {
		kv := strings.Split(i, "=")
		if len(kv) != 2 {
			log.Panicf("error: directive value must be key=value format: %s => %s", t, i)
		}
		directives[kv[0]] = kv[1]
	}

	pathParts := strings.Split(directives["path"], ",")

	fmt.Fprintf(out, "{ rule: \"query(%s: [String!]) { %s @cascade { ", directives["var"], directives["base"])
	for _, p := range pathParts {
		if p != "" {
			fmt.Fprintf(out, "%s { ", p)
		}
	}
	fmt.Fprintf(out, "roles(filter: {group: {in: %s}, permission: {in: [%s]}}) { __typename }", directives["var"], directives["permissions"])
	for _, p := range pathParts {
		if p != "" {
			fmt.Fprintf(out, "}")
		}
	}
	fmt.Fprintf(out, "}}\"},\n")
}

type DropMessage struct {
	DropAll bool `json:"drop_all,omitempty"`
}

func (d DropMessage) encode() []byte {
	bytes, err := json.Marshal(d)
	check(err)
	return bytes
}

func makeRequest(ctx context.Context, url string, method string, authToken string, data io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, data)
	if err != nil {
		return nil, err
	}
	if authToken != "" {
		req.Header.Add(authHeader, authToken)
	}
	return req, err
}

func wipe(ctx context.Context, url string, authToken string) error {
	fmt.Print("Wiping existing schema and data...")

	d := DropMessage{
		DropAll: true,
	}
	req, err := makeRequest(ctx, url+"/alter", http.MethodPost, authToken, bytes.NewReader(d.encode()))
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("wipe returned status %d", resp.StatusCode)
	}

	fmt.Println(" Done.")
	return nil
}

type SchemaResult struct {
	Errors []SchemaResultError `json:"errors,omitempty" yaml:"errors,omitempty"`
}

type SchemaResultError struct {
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

func submitSchema(ctx context.Context, url string, authToken string, schema []byte) error {
	fmt.Print("Submitting schema...")

	req, err := makeRequest(ctx, url+"/admin/schema", http.MethodPost, authToken, bytes.NewReader(schema))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("submit returned status %d", resp.StatusCode)
	}

	r, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var schemaResult SchemaResult
	err = json.Unmarshal(r, &schemaResult)
	if err != nil {
		return err
	}

	if len(schemaResult.Errors) != 0 {
		fmt.Println()
		for _, e := range schemaResult.Errors {
			log.Printf("ERROR: %s", e.Message)
		}
		return fmt.Errorf("Submit returned errors")
	}

	fmt.Println(" Done.")
	return nil
}
