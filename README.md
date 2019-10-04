jctl
====

## Description

jctl (job-control) is a CLI to build and execute Go applications as Kubernetes Job by one command.

## VS.

### kubectl

[kubernetes/kubectl](https://github.com/kubernetes/kubectl) can control all the resources. It creates Job, of course. But, the Job uses already built container images. We have to build an application and a container image and then push it to our container registry.

### ko

[google/ko](https://github.com/google/ko) can build a Go application and container image, push it to registries, and kubectl apply. It make Dockerfile out of consciousness by using Go importpath. We write it in `spec.containers[].image` of a kubernetes manifest.

### jctl

[toshi0607/jctl](https://github.com/toshi0607/jctl) also specializes in Go application and utilize Go importpath. But, we can also remove manifest (only for Job). Write your small Go code and ship it to your cluster as Job!

## Requirement

Go application development environment

### Setup

Set environment variables

* container registry like toshi0607 (Docker Hub) or gcr.io/toshi0607 (Google Container Registry)

```shell script
$ export JCTL_DOCKER_REPO=[your repo]
```

* path to kubernetes config. same manner as kubectl. see also [Organizing Cluster Access Using kubeconfig Files](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)

```shell script
$ export KUBECONFIG=[your kubernetes config]
```

* docker login

## Usage

```shell script
$ cd [path/to/your/application/project/root]
$ jctl ./testdata/cmd/hello_world
building image...
publishing image...
kubernetes: 2019/10/04 10:38:57 job created,  name: jctl-jobxxxxx
kubernetes: 2019/10/04 10:39:00 job finished, name: jctl-jobxxxxx

# or you can use importpath
$ jctl github.com/toshi0607/jctl/testdata/cmd/hello_world
building image...
publishing image...
kubernetes: 2019/10/04 10:38:57 job created,  name: jctl-jobyyyyy
kubernetes: 2019/10/04 10:39:00 job finished, name: jctl-jobyyyyy

# timeout option is available with -t or --timeout [seconds]
$ go run ./cmd/jctl/main.go ./testdata/cmd/long_hello_world -t 5
building image...
publishing image...
kubernetes: 2019/10/04 10:45:21 job created,  name: jctl-jobd9l9b
kubernetes: 2019/10/04 10:45:21 job execution timeout name: jctl-jobzzzzz
job execution timeout: context deadline exceeded
exit status 1
```

## Install

```shell script
$ GO111MODULE=on go get -u github.com/toshi0607/jctl
```

## Licence
[MIT](LICENSE) file for details.

## Author

* [GitHub](https://github.com/toshi0607)
* [twitter](https://twitter.com/toshi0607)
