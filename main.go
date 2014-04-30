package main

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	VERSION = "1.1"
)

var (
	flagVerbose *bool   = flag.Bool("verbose", false, "enable verbose output")
	flagIn      *string = flag.String("in", "", "input skeleton directory or zip file")
	flagDryRun  *bool   = flag.Bool("dry", false, "initate a dry run (i.e. do not create files/dirs)")
	flagOut     *string = flag.String("out", "./__out/", "output directory with the generated structure")
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

	t.outDirBase = fmt.Sprintf("%s-%d", t.Config.Name, time.Now().UnixNano())

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

	outDirBase string // base output directory, which is the skeleton name + random int

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
	x := filepath.Clean(t.Location)
	y := filepath.Clean(path)
	// remove the template location path from the walked path
	// TODO document this ffs
	newp := strings.Replace(y, x, "", -1)

	targetpath := filepath.Join(t.Outdir, t.outDirBase, newp)
	targetpath = t.findReplace(targetpath) // substitute with variables

	if info.IsDir() {
		// create directory
		if *flagVerbose {
			fmt.Println("Creating dir:  ", t.findReplace(targetpath))
		}
		if !t.Dryrun {
			os.MkdirAll(targetpath, 0755)
		}
	} else {
		// create file and substitute
		if *flagVerbose {
			fmt.Println("Creating file: ", targetpath)
		}
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

	return nil
}

// Parses a single skeleton directory, returns a skeleton or an error
// when the skeleton dir did not contain a (valid) config.xml file.
func ParseSkeleton(tdir string) (*Skeleton, error) {
	pathtoconfig := filepath.Join(tdir, "config.xml")
	cfg, err := os.Open(pathtoconfig)
	if err != nil {
		// config file not found, not a skeleton
		return nil, fmt.Errorf("Unable to open skeleton 'config.xml': %s\n", err)
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

// Attempts to unzip the given file to the temp directory. Will return the output
// directory or an error when anything failed.
func Unzip(zipfile string) (gendir string, err error) {
	r, err := zip.OpenReader(zipfile)
	if err != nil {
		return "", err
	}
	defer r.Close()

	// create temp dir
	targetDir, err := ioutil.TempDir("", "skel")
	if err != nil {
		return "", err
	}

	if *flagVerbose {
		fmt.Printf("Using temporary directory '%s'\n", targetDir)
	}

	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return targetDir, err
		}

		// the file or directory to be created
		creationTarget := filepath.Join(targetDir, f.Name)

		// create file in created directory
		if f.FileInfo().IsDir() {
			if *flagVerbose {
				fmt.Printf("Creating directory '%s'\n", f.Name)
			}
			err := os.MkdirAll(creationTarget, 0755)
			if err != nil {
				return targetDir, err
			}
		} else {
			// it's a file, create it.
			newfile, err := os.Create(creationTarget)
			if err != nil {
				return targetDir, err
			}
			if *flagVerbose {
				fmt.Printf("Unzipping file '%s'\n", f.Name)
			}
			_, err = io.Copy(newfile, rc)
			if err != nil {
				return targetDir, err
			}
		}

		rc.Close()
	}

	return targetDir, nil
}

func cleanup(targetFileDir string) {
	if *flagVerbose {
		fmt.Printf("Removing unzip directory '%s'\n", targetFileDir)
	}
	err := os.RemoveAll(targetFileDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to remove directory '%s': %s\n", targetFileDir, err)
		os.Exit(1)
	}

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
		fmt.Fprintf(os.Stderr, "No skeleton specified.\n")
		os.Exit(1)
	}

	fmt.Printf("Opening skeleton '%s'\n", *flagIn)

	// determine type of input (directory or zip file)
	fileOrDir, err := os.Open(*flagIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to open input directory or file '%s': %s\n", *flagIn, err)
		os.Exit(1)
	}

	stat, err := fileOrDir.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to stat '%s': %s\n", *flagIn, err)
		os.Exit(1)
	}

	if *flagDryRun {
		fmt.Printf("This run will not have any effect (dry-run)!\n")
	}

	// indicator whether we used a zipfile or no.
	var isZip bool = false
	var targetFileDir string = *flagIn

	if !stat.IsDir() {
		tdir, err := Unzip(*flagIn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ZIP does not seem to be OK: %s\n", err)
			os.Exit(1)
		}

		isZip = true
		targetFileDir = tdir
	}

	// TODO: clean up the temp dir from the unzipped contents

	t, err := ParseSkeleton(targetFileDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening skeleton: %s\n", err)
		os.Exit(1)
	}

	t.Dryrun = *flagDryRun
	t.Outdir = *flagOut

	fmt.Println()
	fmt.Printf("%s\n", t.Config.Name)
	fmt.Printf("%s\n\n", t.Config.Description)
	fmt.Printf("%d configurable parameter(s) defined:\n", len(t.Config.Parameters))
	if *flagVerbose {
		for _, params := range t.Config.Parameters {
			fmt.Printf("  ${%s}: %s\n", params.Name, params.Description)
		}
	}

	themap := ReadUserInput(t)

	t.KeyValues = themap
	t.Walk()

	if len(t.Unsubstituted) > 0 {
		fmt.Printf("\nWarning: the following variables were left unsubstituted:\n\n")
		for k, _ := range t.Unsubstituted {
			fmt.Printf("\t%s\n", k)
		}
	}

	// remove temporary directory
	if isZip {
		cleanup(targetFileDir)
	}
}
