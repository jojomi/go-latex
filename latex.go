package latex

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/jojomi/go-script"
)

// CompileTask holds the configuration of a compilation
// task
type CompileTask struct {
	scriptContext   *script.Context
	sourceDir       string
	compileDir      string
	compileFilename string
	resolveSymlinks bool
	verbosity       VerbosityLevel
}

type VerbosityLevel uint

const (
	VerbosityNone = iota
	VerbosityDefault
	VerbosityMore
	VerbosityAll
)

// NewCompileTask returns a default (empty) CompileTask
func NewCompileTask() CompileTask {
	return CompileTask{
		verbosity: VerbosityDefault,
	}
}

func (t *CompileTask) context() *script.Context {
	// lazy initialize script context
	if t.scriptContext == nil {
		t.scriptContext = script.NewContext()
	}
	return t.scriptContext
}

// ResolveSymlinks determines if symlinks will be resolved
func (t *CompileTask) ResolveSymlinks() bool {
	return t.resolveSymlinks
}

// SetResolveSymlinks sets if symlinks will be resolved
func (t *CompileTask) SetResolveSymlinks(resolveSymlinks bool) {
	t.resolveSymlinks = resolveSymlinks
}

// SetVerbosity is used to change the verbosity level
func (t *CompileTask) SetVerbosity(verbosity VerbosityLevel) {
	t.verbosity = verbosity
}

// SourceDir returns the source directory for compilation
func (t *CompileTask) SourceDir() string {
	return t.sourceDir
}

// SetSourceDir sets the source directory for compilation
func (t *CompileTask) SetSourceDir(sourceDir string) {
	t.sourceDir = sourceDir
}

// CompileFilename returns the filename to be compiled.
// Makes sure the filename is ending with .tex.
func (t *CompileTask) CompileFilename() string {
	filename := t.compileFilename
	if !strings.HasSuffix(filename, ".tex") {
		return filename + ".tex"
	}
	return filename
}

// CompileFilenamePdf returns the filename of the pdf file
// to be expected after a pdflatex run.
func (t *CompileTask) CompileFilenamePdf() string {
	filename := t.CompileFilename()
	return t.texFilenameToPdf(filename)
}

// SetCompileFilename sets the name of the TeX file to be compiled.
func (t *CompileTask) SetCompileFilename(compileFilename string) {
	t.compileFilename = compileFilename
}

// SetCompileDir sets the directory used for compilation. If no parameter is
// supplied a random and unique temporary directory is used for compilation.
// Usually this is the preferable mode of operation because it ensures clean
// building state.
func (t *CompileTask) SetCompileDir(CompileDir string) {
	if CompileDir == "" {
		CompileDir = t.context().MustGetTempDir()
	}
	t.compileDir = CompileDir

	t.context().SetWorkingDir(t.CompileDirInternal())
}

// CopyToCompileDir copies the source files to the compilation directory.
func (t *CompileTask) CopyToCompileDir(CompileDir string) {
	t.SetCompileDir(CompileDir)

	os.RemoveAll(CompileDir)
	os.MkdirAll(CompileDir, 0700)
	sc := t.context()
	err := sc.CopyDir(t.SourceDir(), t.CompileDirInternal())
	if err != nil {
		panic(err)
	}

	if t.ResolveSymlinks() {
		sc.ResolveSymlinks(t.CompileDirInternal())
	}
}

// ClearCompileDir removes the compilation directory. Suitable to call using
// defer after CopyToCompileDir. Be careful not to remove your source directory
// when building there.
func (t *CompileTask) ClearCompileDir() {
	err := os.RemoveAll(t.CompileDir())
	if err != nil {
		panic(err)
	}
}

func (t *CompileTask) defaultCompileFilename(filename string) (file string) {
	if file == "" {
		file = t.CompileFilename()
	}
	return
}

func (t *CompileTask) texFilenameToPdf(filename string) string {
	return filename[0:len(filename)-4] + ".pdf"
}

func (t *CompileTask) defaultCompilePdfFilename(filename string) (file string) {
	file = t.defaultCompileFilename(filename)
	// replace .tex by .pdf
	file = t.texFilenameToPdf(file)
	return
}

func (t *CompileTask) latextool(toolname, file string, args ...string) error {
	sc := t.context()
	file = t.defaultCompileFilename(file)
	args = append(args, file)

	//fmt.Println(sc.CommandPath("lualatex"))
	sc.MustCommandExist(toolname)
	var execFunction func(string, ...string) error
	switch t.verbosity {
	case VerbosityNone:
		execFunction = sc.ExecuteFullySilent
	case VerbosityMore:
		fallthrough
	case VerbosityAll:
		execFunction = sc.ExecuteDebug
	case VerbosityDefault:
		fallthrough
	default:
		execFunction = sc.ExecuteSilent
	}
	err := execFunction(toolname, args...)
	if err != nil {
		fmt.Print(sc.LastOutput())
		fmt.Print(sc.LastError())
		os.Exit(1)
	}
	return nil
}

// Pdflatex calls pdflatex with the file and arguments supplied. For standard
// invokation no arguments are needed.
func (t *CompileTask) Pdflatex(file string, args ...string) error {
	return t.latextool("pdflatex", file, args...)
}

// Xelatex calls xelatex with the file and arguments supplied. For standard
// invokation no arguments are needed.
func (t *CompileTask) Xelatex(file string, args ...string) error {
	return t.latextool("xelatex", file, args...)
}

// Lualatex calls lualatex with the file and arguments supplied. For standard
// invokation no arguments are needed.
func (t *CompileTask) Lualatex(file string, args ...string) error {
	return t.latextool("lualatex", file, args...)
}

// LillypondBook calls lillypond-book.
func (t *CompileTask) LillypondBook(latexToolname, file string, args ...string) error {
	binName := "lilypond-book"
	sc := t.context()
	file = sc.AbsPath(t.defaultCompileFilename(file))
	tempDir := t.context().MustGetTempDir()
	args = append(args, "--pdf")
	args = append(args, fmt.Sprintf("--output=%s", tempDir))
	args = append(args, file)
	defer os.RemoveAll(tempDir)

	sc.MustCommandExist(binName)
	err := sc.ExecuteFullySilent(binName, args...)
	if err != nil {
		return err
	}

	matches, err := filepath.Glob(filepath.Join(tempDir, "*"))
	if err != nil {
		return err
	}
	for _, match := range matches {
		fi, err := os.Stat(match)
		if err != nil {
			return err
		}
		from := match
		to := path.Join(t.CompileDirInternal(), filepath.Base(match))
		//fmt.Println(from, to)
		if fi.IsDir() {
			err := sc.CopyDir(from, to)
			if err != nil {
				return err
			}
		} else {
			err := sc.CopyFile(from, to)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Optimize modifies a given PDF to reduce filesize for a certain output type.
// Valid values for channel are "screen", "printer", "prepress", "ebook",
// "default".
func (t *CompileTask) Optimize(file string, channel string) error {
	// minify pdf: http://tex.stackexchange.com/a/41273
	// http://stackoverflow.com/a/27454451
	// http://blog.rot13.org/2011/05/optimize-pdf-file-size-using-ghostscript.html
	if !contains([]string{"screen", "printer", "prepress", "ebook", "default"}, channel) {
		// TODO err?
		return nil
	}

	sc := t.context()
	if !sc.CommandExists("gs") {
		return nil
	}

	file = t.defaultCompilePdfFilename(file)
	tempFile := sc.MustGetTempFile()
	params := []string{
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.4",
		fmt.Sprintf("-dPDFSETTINGS=/%s", channel),
		"-o",
		tempFile.Name(),
		file,
	}
	sc.SetWorkingDir(t.CompileDirInternal())
	err := sc.ExecuteSilent("gs", params...)
	if err != nil {
		return err
	}
	sc.MoveFile(tempFile.Name(), file)
	return nil
}

// MoveToDest moves a file from compilation directory.
func (t *CompileTask) MoveToDest(from, to string) error {
	from = t.defaultCompilePdfFilename(from)
	from = path.Join(t.CompileDirInternal(), from)
	to, err := filepath.Abs(to)
	if err != nil {
		panic(err)
	}
	return t.context().MoveFile(from, to)
}

// CompileDir returns the current compilation directory.
func (t *CompileTask) CompileDir() string {
	if t.compileDir == "" {
		return t.sourceDir
	}
	return t.compileDir
}

// CompileDirInternal returns the current internal compilation directory.
func (t *CompileTask) CompileDirInternal() string {
	if t.CompileDir() == t.SourceDir() {
		return t.CompileDir()
	}
	return path.Join(t.CompileDir(), "input")
}

// ClearLatexTempFiles removes common temprary LaTeX files in a directory.
func (t *CompileTask) ClearLatexTempFiles(dir string) {
	// remove temp files
	extensions := []string{"aux", "log", "toc", "nav", "ind", "ilg", "idx"}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		for _, ext := range extensions {
			if strings.HasSuffix(path, "."+ext) {
				os.Remove(path)
				return nil
			}
		}
		return nil
	})
}

// Template returns a text/template to base templating off.
func (t *CompileTask) Template(baseFilename string) (*template.Template, string) {
	sc := t.context()
	baseFilename = sc.AbsPath(t.defaultCompileFilename(baseFilename))
	templ := template.New("latex")
	return templ, baseFilename
}

// ExecuteTemplate executes a template on the source TeX files.
func (t *CompileTask) ExecuteTemplate(templ *template.Template, data interface{}, inputFilename string, outputFilename string) {
	sc := t.context()

	useTempFile := outputFilename == ""
	if useTempFile {
		outputFilename = sc.MustGetTempFile().Name()
	}
	inputFilename = sc.AbsPath(t.defaultCompileFilename(inputFilename))

	f, err := os.Create(sc.AbsPath(outputFilename))
	if err != nil {
		panic(err)
	}
	w := io.Writer(f)
	err = templ.ExecuteTemplate(w, filepath.Base(inputFilename), data)
	if err != nil {
		panic(err)
	}
	f.Close()

	if useTempFile {
		// copy back, remove temp
		err = os.Remove(inputFilename)
		if err != nil {
			panic(err)
		}
		err = sc.CopyFile(outputFilename, inputFilename)
		if err != nil {
			panic(err)
		}
	}
}

// auxiliary
func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
