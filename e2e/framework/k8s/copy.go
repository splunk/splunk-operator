package k8s

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// CopyFileToPod copies a local file to a pod path.
func (c *Client) CopyFileToPod(ctx context.Context, namespace, podName, srcPath, destPath string) (string, string, error) {
	reader, writer := io.Pipe()
	if destPath != "/" && strings.HasSuffix(string(destPath[len(destPath)-1]), "/") {
		destPath = destPath[:len(destPath)-1]
	}

	go func() {
		defer writer.Close()
		_ = cpMakeTar(newLocalPath(srcPath), newRemotePath(destPath), writer)
	}()

	cmdArr := []string{"tar", "-xf", "-"}
	destDir := path.Dir(destPath)
	if len(destDir) > 0 {
		cmdArr = append(cmdArr, "-C", destDir)
	}

	pod := &corev1.Pod{}
	if err := c.Client.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return "", "", err
	}

	gvk, _ := apiutil.GVKForObject(pod, c.Scheme)
	restClient, err := apiutil.RESTClientForGVK(gvk, false, c.RestConfig, serializer.NewCodecFactory(c.Scheme), http.DefaultClient)
	if err != nil {
		return "", "", err
	}

	execReq := restClient.Post().Resource("pods").Name(podName).Namespace(namespace).SubResource("exec")
	option := &corev1.PodExecOptions{
		Command: cmdArr,
		Stdin:   true,
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}

	execReq.VersionedParams(option, runtime.NewParameterCodec(c.Scheme))
	exec, err := remotecommand.NewSPDYExecutor(c.RestConfig, "POST", execReq.URL())
	if err != nil {
		return "", "", err
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	if err := exec.Stream(remotecommand.StreamOptions{
		Stdin:  reader,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	}); err != nil {
		return "", "", err
	}

	return stdout.String(), stderr.String(), nil
}

func cpMakeTar(src localPath, dest remotePath, writer io.Writer) error {
	tarWriter := tar.NewWriter(writer)
	defer tarWriter.Close()

	srcPath := src.Clean()
	destPath := dest.Clean()
	return recursiveTar(srcPath.Dir(), srcPath.Base(), destPath.Dir(), destPath.Base(), tarWriter)
}

func recursiveTar(srcDir, srcFile localPath, destDir, destFile remotePath, tw *tar.Writer) error {
	matchedPaths, err := srcDir.Join(srcFile).Glob()
	if err != nil {
		return err
	}
	for _, fpath := range matchedPaths {
		stat, err := os.Lstat(fpath)
		if err != nil {
			return err
		}
		if stat.IsDir() {
			files, err := os.ReadDir(fpath)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				hdr, _ := tar.FileInfoHeader(stat, fpath)
				hdr.Name = destFile.String()
				if err := tw.WriteHeader(hdr); err != nil {
					return err
				}
			}
			for _, f := range files {
				if err := recursiveTar(srcDir, srcFile.Join(newLocalPath(f.Name())), destDir, destFile.Join(newRemotePath(f.Name())), tw); err != nil {
					return err
				}
			}
			return nil
		} else if stat.Mode()&os.ModeSymlink != 0 {
			hdr, _ := tar.FileInfoHeader(stat, fpath)
			target, err := os.Readlink(fpath)
			if err != nil {
				return err
			}

			hdr.Linkname = target
			hdr.Name = destFile.String()
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
		} else {
			hdr, err := tar.FileInfoHeader(stat, fpath)
			if err != nil {
				return err
			}
			hdr.Name = destFile.String()
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}

			f, err := os.Open(fpath)
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
			return f.Close()
		}
	}
	return nil
}
