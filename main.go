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
	flagIn     *string = flag.String("in", "", "input skeleton directory")
	flagDryRun *bool   = flag.Bool("dry", false, "initate a dry run (i.e. do not create files/dirs)")
	flagOut    *string = flag.String("out", "./__out/", "output directory with the generated structure")
)

func usage() {
	fmt.Fprintf(os.Stderr, "%s v%s\n\n", os.Args[0], VERSION)
	fmt.Fprintf(os.Stderr, "Generates directories, files and contents based on a 'skeleton' structure.\n")
	fmt.Fprintf(os.Stderr, "All values in the form of ${x} are substituted, in directory/file names,\n")
	fmt.Fprintf(os.Stderr, "but also in content of files. The values for these variables are requested\n")
	fmt.Fprintf(os.Stderr, "on the standard input when a correct skeleton input is specified.\n\n")

	fmt.Fprintf(os.Stderr, "Usage:\n\n")
	flag.PrintDefaults()
}

// Skeleton configuration XML file
type SkeletonConfig struct {
	Name        string           `xml:"name"`
	Description string           `xml:"description"`
	Parameters  []SkeletonParams `xml:"parameters>param"`
}

type SkeletonParams struct {
	Name        string `xml:"name,attr"`
	Description string `xml:"description,attr"`
}

func NewSkeleton(location string, config SkeletonConfig) *Skeleton {
	t := new(Skeleton)
	t.Location = location
	t.Config = config
	t.regex = regexp.MustCompile("\\${(.+)}")
	t.Unsubstituted = make(map[string]bool)
	return t
}

// Basic skeleton structure.
type Skeleton struct {
	Location      string            // location of the skeleton
	Config        SkeletonConfig    // skeleton configuration (parsed from XML)
	Outdir        string            // Output directory
	Dryrun        bool              // whether it's a dry run, without output
	KeyValues     map[string]string // substitutable keys and their values
	Unsubstituted map[string]bool   // Unsubstituted particles

	regex *regexp.Regexp
}

// Finds occurences in the src string of ${..} vars and will substitute them
// with any given values in the KeyValues map.
func (t Skeleton) findReplace(src string) string {
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

func (t Skeleton) Walk() {
	filepath.Walk(t.Location, t.walkFunc)
}

func (t Skeleton) walkFunc(path string, info os.FileInfo, err error) error {
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

// Parses a single skeleton directory, returns a skeleton or an error
// when the skeleton dir did not contain a (valid) config.xml file.
func ParseSkeleton(tdir string) (*Skeleton, error) {
	pathtoconfig := filepath.Join(tdir, "config.xml")
	cfg, err := os.Open(pathtoconfig)
	if err != nil {
		// config file not found, not a skeleton
		return nil, err
	}

	confData, err := ioutil.ReadAll(cfg)
	if err != nil {
		return nil, err
	}

	tmplConfig := SkeletonConfig{}
	xml.Unmarshal(confData, &tmplConfig)

	location := filepath.Dir(cfg.Name())

	skeleton := NewSkeleton(location, tmplConfig)

	return skeleton, nil
}

// Reads user input from stdin to get a map with param names and their values.
func ReadUserInput(t *Skeleton) map[string]string {
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

	if *flagIn == "" {
		fmt.Fprintf(os.Stderr, "No skeleton specified")
		os.Exit(1)
	}

	fmt.Printf("Opening skeleton '%s'\n", *flagIn)
	t, err := ParseSkeleton(*flagIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening skeleton: %s\n", err)
		os.Exit(1)
	}

	if *flagDryRun {
		fmt.Printf("This run will not have any effect (dry-run)!\n\n")
	}

	t.Dryrun = *flagDryRun
	t.Outdir = *flagOut

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
