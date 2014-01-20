package main

import (
	"bufio"
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	VERSION = "1.0"
)

var (
	flagTemplate  *string = flag.String("template", "", "template to execute")
	flagDryRun    *bool   = flag.Bool("dry", false, "initate a dry run (i.e. do not create files/dirs)")
	flagOutputDir *string = flag.String("output", "./__out/", "output directory")
)

func usage() {
	fmt.Fprintf(os.Stderr, "%s v%s\n\n", os.Args[0], VERSION)
	fmt.Fprintf(os.Stderr, "Generates directories, files and contents based on a 'template' structure.\n")
	fmt.Fprintf(os.Stderr, "All values in the form of ${x} are substituted. Values for these variables\n")
	fmt.Fprintf(os.Stderr, "are requested on stdin when a correct template is specified.\n\n")

	fmt.Fprintf(os.Stderr, "Usage:\n\n")
	flag.PrintDefaults()
}

// Template configuration XML file
type TemplateConfig struct {
	Name        string           `xml:"name"`
	Description string           `xml:"description"`
	Parameters  []TemplateParams `xml:"parameters>param"`
}

type TemplateParams struct {
	Name        string `xml:"name,attr"`
	Description string `xml:"description,attr"`
}

func NewTemplate(location string, config TemplateConfig) *Template {
	t := new(Template)
	t.Location = location
	t.Config = config
	t.regex = regexp.MustCompile("\\${(.+)}")
	t.Unsubstituted = make(map[string]bool)
	return t
}

// Basic template structure.
type Template struct {
	Location      string            // location of the template
	Config        TemplateConfig    // template configuration (parsed from XML)
	Outdir        string            // Output directory
	Dryrun        bool              // whether it's a dry run, without output
	KeyValues     map[string]string // substitutable keys and their values
	Unsubstituted map[string]bool   // Unsubstituted particles

	regex *regexp.Regexp
}

// Finds occurences in the src string of ${..} vars and will substitute them
// with any given values in the KeyValues map.
func (t Template) findReplace(src string) string {
	for k, v := range t.KeyValues {
		haha := fmt.Sprintf("${%s}", k)
		src = strings.Replace(src, haha, v, -1)
	}
	// check for unprocessed replacements
	strfound := t.regex.FindString(src)
	if strfound != "" {
		t.Unsubstituted[strfound] = true
	}

	return src
}

func (t Template) Walk() {
	filepath.Walk(t.Location, t.walkFunc)
}

func (t Template) walkFunc(path string, info os.FileInfo, err error) error {
	// cut down the first two elements from the path to create the target portion
	split := strings.Split(path, string(os.PathSeparator))

	if len(split) > 2 {
		targetpath := filepath.Join(split[2:]...)
		targetpath = filepath.Join(t.Outdir, targetpath)
		targetpath = t.findReplace(targetpath) // substitute with variables

		if info.IsDir() {
			// create directory
			fmt.Println("Creating dir:  ", t.findReplace(targetpath))
			if !t.Dryrun {
				os.MkdirAll(targetpath, os.ModeDir)
			}
		} else {
			// create file and substitute
			fmt.Println("Creating file: ", targetpath)
			if !t.Dryrun {
				os.Create(targetpath)
			}
			// read original contents, write contents
			origBytes, err := ioutil.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to open file '%s': %s\n", path, err)
				return nil
			}

			if !t.Dryrun {
				newcontents := t.findReplace(string(origBytes))
				ioutil.WriteFile(targetpath, []byte(newcontents), os.ModePerm)
			}

		}
	}
	return nil
}

// Parses a single template directory, returns a template or an error
// when the template dir did not contain a (valid) config.xml file.
func ParseTemplate(tdir string) (*Template, error) {
	pathtoconfig := filepath.Join(tdir, "config.xml")
	cfg, err := os.Open(pathtoconfig)
	if err != nil {
		// config file not found, not a template
		return nil, err
	}

	confData, err := ioutil.ReadAll(cfg)
	if err != nil {
		return nil, err
	}

	tmplConfig := TemplateConfig{}
	xml.Unmarshal(confData, &tmplConfig)

	location := filepath.Dir(cfg.Name())

	template := NewTemplate(location, tmplConfig)

	return template, nil
}

// Reads user input from stdin to get a map with param names and their values.
func ReadUserInput(t *Template) map[string]string {
	paramvals := make(map[string]string)

	bio := bufio.NewReader(os.Stdin)

	fmt.Println()

	for _, p := range t.Config.Parameters {
		fmt.Printf("%s: \n> ", p.Description)
		bline, _, _ := bio.ReadLine()

		paramvals[p.Name] = string(bline)
	}

	fmt.Printf("\nThe following parameters are specified:\n\n")

	for k, v := range paramvals {
		fmt.Printf("%s = %s\n", k, v)
	}

	fmt.Println()

	return paramvals
}

// Start of this heap.
func main() {
	flag.Usage = usage
	flag.Parse()
	if !flag.Parsed() {
		flag.Usage()
		os.Exit(1)
	}

	if *flagTemplate == "" {
		fmt.Fprintf(os.Stderr, "No template specified")
		os.Exit(1)
	}

	fmt.Printf("Opening template '%s'\n", *flagTemplate)
	t, err := ParseTemplate(*flagTemplate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening template: %s\n", err)
		os.Exit(1)
	}

	if *flagDryRun {
		fmt.Printf("This run will not have any effect (dry-run)!\n\n")
	}

	t.Dryrun = *flagDryRun
	t.Outdir = *flagOutputDir

	fmt.Printf("%s: %s\n", t.Config.Name, t.Config.Description)
	fmt.Printf("%d configurable parameter(s) defined\n", len(t.Config.Parameters))

	themap := ReadUserInput(t)

	t.KeyValues = themap
	t.Walk()

	if len(t.Unsubstituted) > 0 {
		fmt.Printf("\nWarning: the following variables were left unsubstituted:\n\n")
		for k, _ := range t.Unsubstituted {
			fmt.Printf("\t%s\n", k)
		}
	}
}
