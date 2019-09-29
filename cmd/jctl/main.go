package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/toshi0607/jctl/gobuild"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	builder, err := gobuild.MakeBuilder()
	if err != nil {
		fmt.Println(err)
	}
	publisher, err := gobuild.NewDefault()
	if err != nil {
		fmt.Println(err)
	}
	ref, err := gobuild.PublishImages("github.com/toshi0607/jctl/testdata/cmd/test", publisher, builder)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("published")

	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "kind-config-kind"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	// construct job
	// TODO: namespaceはフラグ指定
	jobName := "jctl-job"
	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: jobName,
			Namespace:    "default",
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  jobName,
							Image: ref.Name(),
						},
					},
					ImagePullSecrets: []corev1.LocalObjectReference{{Name: "image-puller"}},
					RestartPolicy:    corev1.RestartPolicyNever,
				},
			},
		},
	}
	createdJob, err := clientset.BatchV1().Jobs("default").Create(job)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(createdJob)

	w, err := clientset.BatchV1().Jobs("default").Watch(metav1.ListOptions{})
	defer w.Stop()
	ch := w.ResultChan()
	for {
		select {
		case obj := <-ch:
			job, ok := obj.Object.(*batchv1.Job)
			if !ok {
				fmt.Printf("who are you? %v", obj)
			}
			if createdJob.Name == job.Name &&
				IsJobFinished(job) {
				fmt.Printf("Job: %s finished", createdJob.Name)
				return
			}
		}
	}
}

func IsJobFinished(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// TODO: check KUBECONFIG as well
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}
