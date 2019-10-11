package kubernetes

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	imagePullSecretName = "image-puller"
	jobName             = "jctl-job"
)

type JobCli interface {
	Create(ctx context.Context, image string) error
}

type (
	jobCli struct {
		log       *log.Logger
		Clientset *kubernetes.Clientset
		Namespace string
	}
)

func New(outStream io.Writer, ns, kc string) (JobCli, error) {
	log := log.New(outStream, "kubernetes: ", log.LstdFlags)

	kubeConfig, err := getKubeConfig(kc)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get kubeconfig")
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build config")
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create clientset")
	}

	return &jobCli{
		log:       log,
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
	if home, err := os.UserHomeDir(); home != "" && err != nil {
		return filepath.Join(home, ".kube", "config"), nil
	}
	return "", errors.New("kubectx not found")
}

func (c *jobCli) Create(ctx context.Context, image string) error {
	job := buildJob(image, c.Namespace)
	createdJob, err := c.Clientset.BatchV1().Jobs(c.Namespace).Create(job)
	if err != nil {
		errors.Wrapf(err, "failed to create batch, namespace: %s, image: %s", c.Namespace, image)
	}
	c.log.Printf("job created,  name: %s", createdJob.Name)

	w, err := c.Clientset.BatchV1().Jobs(c.Namespace).Watch(metav1.ListOptions{})
	defer w.Stop()
	ch := w.ResultChan()
	for {
		select {
		case <-ctx.Done():
			c.log.Printf("job execution timeout name: %s\n", createdJob.Name)
			return errors.Wrap(ctx.Err(), "job execution timeout")
		case obj := <-ch:
			job, ok := obj.Object.(*batchv1.Job)
			if !ok {
				c.log.Printf("unexpected kind object: %v", obj)
			}
			if createdJob.Name == job.Name && isFinished(job) {
				c.log.Printf("job finished, name: %s\n", createdJob.Name)
				return nil
			}
		}
	}
}

func buildJob(image, namespace string) *batchv1.Job {
	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: jobName,
			Namespace:    namespace,
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
					ImagePullSecrets: []corev1.LocalObjectReference{{Name: imagePullSecretName}},
					RestartPolicy:    corev1.RestartPolicyNever,
				},
			},
		},
	}
}

func isFinished(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
