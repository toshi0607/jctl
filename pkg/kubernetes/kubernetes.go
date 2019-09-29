package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"
)

type JobCli interface {
	Create(ctx context.Context, image string) error
}

type (
	jobCli struct {
		OutStream, ErrStream io.Writer
		Clientset            *kubernetes.Clientset
		Namespace            string
	}
)

func New(outStream, errStream io.Writer, ns, kc string) (JobCli, error) {
	kubeConfig, err := getKubeConfig(kc)
	if err != nil {
		return nil, err
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		log.Fatal(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	return &jobCli{
		OutStream: outStream,
		ErrStream: errStream,
		Namespace: ns,
		Clientset: clientset,
	}, nil
}

func getKubeConfig(kc string) (string, error) {
	if kc != "" {
		return kc, nil
	}
	if os.Getenv("KUBECONFIG") != "" {
		return os.Getenv("KUBECONFIG"), nil
	}
	if defaultConfig() != "" {
		return defaultConfig(), nil
	}
	return "", errors.New("kubectx not found")
}

func defaultConfig() string {
	if home := homeDir(); home != "" {
		return filepath.Join(home, ".kube", "config")
	}
	return ""
}

func IsJobFinished(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	// windows
	return os.Getenv("USERPROFILE")
}

func (c *jobCli) Create(ctx context.Context, image string) error {
	jobName := "jctl-job"
	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: jobName,
			Namespace:    c.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  jobName,
							Image: image,
						},
					},
					ImagePullSecrets: []corev1.LocalObjectReference{{Name: "image-puller"}},
					RestartPolicy:    corev1.RestartPolicyNever,
				},
			},
		},
	}
	createdJob, err := c.Clientset.BatchV1().Jobs(c.Namespace).Create(job)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(createdJob)

	w, err := c.Clientset.BatchV1().Jobs(c.Namespace).Watch(metav1.ListOptions{})
	defer w.Stop()
	ch := w.ResultChan()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case obj := <-ch:
			job, ok := obj.Object.(*batchv1.Job)
			if !ok {
				fmt.Printf("who are you? %v", obj)
			}
			if createdJob.Name == job.Name &&
				IsJobFinished(job) {
				fmt.Printf("Job: %s finished", createdJob.Name)
				return nil
			}
		}
	}
}
