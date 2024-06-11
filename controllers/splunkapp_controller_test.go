package controllers

import (
	"context"
	//"fmt"
	"io/ioutil"
	"os"
	//"path/filepath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"testing"
	//"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
)

func TestSplunkAppReconciler_Reconcile(t *testing.T) {
	type fields struct {
		client client.Client
		scheme *runtime.Scheme
	}
	type args struct {
		req reconcile.Request
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    reconcile.Result
		wantErr bool
		setup   func(t *testing.T, client client.Client)
	}{
		{
			name: "SplunkApp not found",
			fields: fields{
				client: fake.NewClientBuilder().WithScheme(scheme.Scheme).Build(),
				scheme: scheme.Scheme,
			},
			args: args{
				req: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "splunk-app",
					},
				},
			},
			want:    reconcile.Result{},
			wantErr: false,
			setup: func(t *testing.T, client client.Client) {
				// No setup required
			},
		},
		{
			name: "SplunkApp found",
			fields: fields{
				client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(
					&enterprisev4.SplunkApp{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "splunk-app",
						},
						Spec: enterprisev4.SplunkAppSpec{
							ConfigFiles: []enterprisev4.ConfigFile{
								{
									ConfigMapRef: corev1.ObjectReference{
										Namespace: "default",
										Name:      "splunk-app-config",
									},
									RelativePath:   "conf",
									ConfigFileName: "inputs.conf",
								},
							},
						},
					},
					&corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "splunk-app-config",
						},
						Data: map[string]string{
							"inputs.conf": "inputs = stdin",
						},
					},
				).Build(),
				scheme: scheme.Scheme,
			},
			args: args{
				req: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "splunk-app",
					},
				},
			},
			want:    reconcile.Result{},
			wantErr: false,
			setup: func(t *testing.T, client client.Client) {
				// No setup required
			},
		},
		{
			name: "Referenced ConfigMap not found",
			fields: fields{
				client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(
					&enterprisev4.SplunkApp{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "splunk-app",
						},
						Spec: enterprisev4.SplunkAppSpec{
							ConfigFiles: []enterprisev4.ConfigFile{
								{
									ConfigMapRef: corev1.ObjectReference{
										Namespace: "default",
										Name:      "splunk-app-config",
									},
									RelativePath:   "conf",
									ConfigFileName: "inputs.conf",
								},
							},
						},
					},
				).Build(),
				scheme: scheme.Scheme,
			},
			args: args{
				req: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "splunk-app",
					},
				},
			},
			want:    reconcile.Result{},
			wantErr: true,
			setup: func(t *testing.T, client client.Client) {
				// No setup required
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t, tt.fields.client)
			r := &SplunkAppReconciler{
				Client: tt.fields.client,
				Scheme: tt.fields.scheme,
			}
			got, err := r.Reconcile(context.Background(), tt.args.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SplunkAppReconciler.Reconcile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !assert.Equal(t, tt.want, got) {
				t.Errorf("SplunkAppReconciler.Reconcile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_createTarGz(t *testing.T) {
	type args struct {
		src string
		dst string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		setup   func(t *testing.T)
		cleanup func(t *testing.T)
	}{
		{
			name: "Create tar.gz file",
			args: args{
				src: "/tmp/splunk-app",
				dst: "/tmp/splunk-app.tar.gz",
			},
			wantErr: false,
			setup: func(t *testing.T) {
				err := os.MkdirAll("/tmp/splunk-app/conf", os.ModePerm)
				require.NoError(t, err)
				err = ioutil.WriteFile("/tmp/splunk-app/conf/inputs.conf", []byte("inputs = stdin"), os.ModePerm)
				require.NoError(t, err)
			},
			cleanup: func(t *testing.T) {
				os.RemoveAll("/tmp/splunk-app")
				os.Remove("/tmp/splunk-app.tar.gz")
			},
		},
		{
			name: "Source directory does not exist",
			args: args{
				src: "/tmp/non-existent-dir",
				dst: "/tmp/non-existent-dir.tar.gz",
			},
			wantErr: true,
			setup:   func(t *testing.T) {},
			cleanup: func(t *testing.T) {
				os.Remove("/tmp/non-existent-dir.tar.gz")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			if err := createTarGz(tt.args.src, tt.args.dst); (err != nil) != tt.wantErr {
				t.Errorf("createTarGz() error = %v, wantErr %v", err, tt.wantErr)
			}
			tt.cleanup(t)
		})
	}
}

func Test_createTarGz_with_multiple_files(t *testing.T) {
	type args struct {
		src string
		dst string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		setup   func(t *testing.T)
		cleanup func(t *testing.T)
	}{
		{
			name: "Create tar.gz file with multiple files",
			args: args{
				src: "/tmp/splunk-app",
				dst: "/tmp/splunk-app.tar.gz",
			},
			wantErr: false,
			setup: func(t *testing.T) {
				err := os.MkdirAll("/tmp/splunk-app/conf", os.ModePerm)
				require.NoError(t, err)
				err = ioutil.WriteFile("/tmp/splunk-app/conf/inputs.conf", []byte("inputs = stdin"), os.ModePerm)
				require.NoError(t, err)
				err = os.MkdirAll("/tmp/splunk-app/local", os.ModePerm)
				require.NoError(t, err)
				err = ioutil.WriteFile("/tmp/splunk-app/local/inputs.conf", []byte("inputs = stdin"), os.ModePerm)
				require.NoError(t, err)
			},
			cleanup: func(t *testing.T) {
				os.RemoveAll("/tmp/splunk-app")
				os.Remove("/tmp/splunk-app.tar.gz")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			if err := createTarGz(tt.args.src, tt.args.dst); (err != nil) != tt.wantErr {
				t.Errorf("createTarGz() error = %v, wantErr %v", err, tt.wantErr)
			}
			tt.cleanup(t)
		})
	}
}

func Test_createTarGz_with_subdirectories(t *testing.T) {
	type args struct {
		src string
		dst string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		setup   func(t *testing.T)
		cleanup func(t *testing.T)
	}{
		{
			name: "Create tar.gz file with subdirectories",
			args: args{
				src: "/tmp/splunk-app",
				dst: "/tmp/splunk-app.tar.gz",
			},
			wantErr: false,
			setup: func(t *testing.T) {
				err := os.MkdirAll("/tmp/splunk-app/conf/inputs", os.ModePerm)
				require.NoError(t, err)
				err = ioutil.WriteFile("/tmp/splunk-app/conf/inputs/inputs.conf", []byte("inputs = stdin"), os.ModePerm)
				require.NoError(t, err)
				err = os.MkdirAll("/tmp/splunk-app/local/inputs", os.ModePerm)
				require.NoError(t, err)
				err = ioutil.WriteFile("/tmp/splunk-app/local/inputs/inputs.conf", []byte("inputs = stdin"), os.ModePerm)
				require.NoError(t, err)
			},
			cleanup: func(t *testing.T) {
				os.RemoveAll("/tmp/splunk-app")
				os.Remove("/tmp/splunk-app.tar.gz")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			if err := createTarGz(tt.args.src, tt.args.dst); (err != nil) != tt.wantErr {
				t.Errorf("createTarGz() error = %v, wantErr %v", err, tt.wantErr)
			}
			tt.cleanup(t)
		})
	}
}

func Test_createTarGz_with_empty_directory(t *testing.T) {
	type args struct {
		src string
		dst string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		setup   func(t *testing.T)
		cleanup func(t *testing.T)
	}{
		{
			name: "Create tar.gz file with empty directory",
			args: args{
				src: "/tmp/splunk-app",
				dst: "/tmp/splunk-app.tar.gz",
			},
			wantErr: false,
			setup: func(t *testing.T) {
				err := os.MkdirAll("/tmp/splunk-app/conf", os.ModePerm)
				require.NoError(t, err)
			},
			cleanup: func(t *testing.T) {
				os.RemoveAll("/tmp/splunk-app")
				os.Remove("/tmp/splunk-app.tar.gz")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			if err := createTarGz(tt.args.src, tt.args.dst); (err != nil) != tt.wantErr {
				t.Errorf("createTarGz() error = %v, wantErr %v", err, tt.wantErr)
			}
			tt.cleanup(t)
		})
	}
}

func Test_createTarGz_with_symlink(t *testing.T) {
	type args struct {
		src string
		dst string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		setup   func(t *testing.T)
		cleanup func(t *testing.T)
	}{
		{
			name: "Create tar.gz file with symlink",
			args: args{
				src: "/tmp/splunk-app",
				dst: "/tmp/splunk-app.tar.gz",
			},
			wantErr: false,
			setup: func(t *testing.T) {
				err := os.MkdirAll("/tmp/splunk-app/conf", os.ModePerm)
				require.NoError(t, err)
				err = ioutil.WriteFile("/tmp/splunk-app/conf/inputs.conf", []byte("inputs = stdin"), os.ModePerm)
				require.NoError(t, err)
				err = os.Symlink("/tmp/splunk-app/conf/inputs.conf", "/tmp/splunk-app/local/inputs.conf")
				require.NoError(t, err)
			},
			cleanup: func(t *testing.T) {
				os.RemoveAll("/tmp/splunk-app")
				os.Remove("/tmp/splunk-app.tar.gz")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			if err := createTarGz(tt.args.src, tt.args.dst); (err != nil) != tt.wantErr {
				t.Errorf("createTarGz() error = %v, wantErr %v", err, tt.wantErr)
			}
			tt.cleanup(t)
		})
	}
}

func Test_createTarGz_with_relative_path(t *testing.T) {
	type args struct {
		src string
		dst string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		setup   func(t *testing.T)
		cleanup func(t *testing.T)
	}{
		{
			name: "Create tar.gz file with relative path",
			args: args{
				src: "/tmp/splunk-app",
				dst: "/tmp/splunk-app.tar.gz",
			},
			wantErr: false,
			setup: func(t *testing.T) {
				err := os.MkdirAll("/tmp/splunk-app/conf", os.ModePerm)
				require.NoError(t, err)
				err = ioutil.WriteFile("/tmp/splunk-app/conf/inputs.conf", []byte("inputs = stdin"), os.ModePerm)
				require.NoError(t, err)
				err = os.MkdirAll("/tmp/splunk-app/local", os.ModePerm)
				require.NoError(t, err)
				err = ioutil.WriteFile("/tmp/splunk-app/local/inputs.conf", []byte("inputs = stdin"), os.ModePerm)
				require.NoError(t, err)
			},
			cleanup: func(t *testing.T) {
				os.RemoveAll("/tmp/splunk-app")
				os.Remove("/tmp/splunk-app.tar.gz")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			if err := createTarGz(tt.args.src, tt.args.dst); (err != nil) != tt.wantErr {
				t.Errorf("createTarGz() error = %v, wantErr %v", err, tt.wantErr)
			}
			tt.cleanup(t)
		})
	}
}
