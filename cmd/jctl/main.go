package main

import (
	"flag"
	"fmt"
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

func main() {
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	clientser, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	// construct job
	jobName := "job1"
	job := &batchv1.Job{
		//TypeMeta: metav1.TypeMeta{APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			//CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    jobName,
							Image:   "busybox",
							Command: []string{"date"},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	job, err = clientser.BatchV1().Jobs("default").Create(job)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(job)
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}
