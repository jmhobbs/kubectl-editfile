package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/fsnotify/fsnotify"
)

type RemoteFile struct {
	Binary    string
	Namespace string
	Pod       string
	Container string
	Path      string
	local     string
	tmp       string
}

func main() {
	file, err := New()
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	flag.StringVar(&file.Binary, "kubectl", "kubectl", "Path to the kubectl binary")
	flag.StringVar(&file.Namespace, "n", "", "If present, the namespace scope for this CLI request")
	flag.StringVar(&file.Container, "c", "", "Container name. If omitted, the first container in the pod will be chosen")

	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: kubectl editfile [-n namespace] [-c container] <pod>:<path>")
		fmt.Fprintln(flag.CommandLine.Output(), "Edit a remote file, uploaded on save.")
		flag.PrintDefaults()
	}

	flag.Parse()

	file.Pod, file.Path = splitPodAndPath(flag.Arg(0))
	if file.Pod == "" || file.Path == "" {
		log.Fatal("invalid or missing pod and path")
	}

	err = file.Download(os.Stdout, os.Stderr)
	if err != nil {
		log.Fatal(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nano"
	}
	editCmd := exec.Command(editor, file.Local())
	editCmd.Stdin = os.Stdin
	editCmd.Stdout = os.Stdout
	editCmd.Stderr = os.Stderr
	editCmd.Start()

	editorExited := make(chan bool)
	go func() {
		editCmd.Wait()
		editorExited <- true
	}()

	err = watcher.Add(file.Local())
	if err != nil {
		if kerr := editCmd.Process.Kill(); kerr != nil {
			log.Println("error stopping editor:", kerr)
		}
		log.Fatal(err)
	}

	shutdown := false
	for {
		select {
		case <-editorExited:
			shutdown = true
			break
		case event, ok := <-watcher.Events:
			if !ok {
				break
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				if err = file.Upload(nil, nil); err != nil {
					if !editCmd.ProcessState.Exited() {
						if kerr := editCmd.Process.Kill(); kerr != nil {
							log.Println("error stopping editor:", kerr)
						}
					}
					log.Fatal(err)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				break
			}
			if !editCmd.ProcessState.Exited() {
				if kerr := editCmd.Process.Kill(); kerr != nil {
					log.Println("error stopping editor:", kerr)
				}
			}
			log.Fatal(err)
		}

		if shutdown {
			log.Println("Shutting down.")
			break
		}
	}
}

func splitPodAndPath(s string) (string, string) {
	i := strings.Index(s, ":")
	if i == -1 {
		return "", ""
	}

	return s[:i], s[i+1:]
}

func New() (*RemoteFile, error) {
	tmp, err := ioutil.TempDir("", "kubectl_editfile")
	if err != nil {
		return nil, err
	}

	return &RemoteFile{
		tmp:   tmp,
		local: path.Join(tmp, "edit"),
	}, nil
}

func (r *RemoteFile) baseArgs() []string {
	args := []string{"cp"}
	if "" != r.Namespace {
		args = append(args, "-n", r.Namespace)
	}
	if "" != r.Container {
		args = append(args, "-c", r.Container)
	}
	return args
}

func (r *RemoteFile) remoteSpec() string {
	return fmt.Sprintf("%s:%s", r.Pod, r.Path)
}

func (r *RemoteFile) Close() error {
	return os.RemoveAll(r.tmp)
}

func (r *RemoteFile) Local() string {
	return r.local
}

func (r *RemoteFile) Download(stdout, stderr io.Writer) error {
	args := append(r.baseArgs(), r.remoteSpec(), r.Local())
	cmd := exec.Command(r.Binary, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func (r *RemoteFile) Upload(stdout, stderr io.Writer) error {
	args := append(r.baseArgs(), r.Local(), r.remoteSpec())
	stderr.Write([]byte(fmt.Sprintf("%s %s", r.Binary, strings.Join(args, " "))))
	stderr.Write([]byte{'\n'})
	cmd := exec.Command(r.Binary, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
