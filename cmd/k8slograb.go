package main

import (
    "bufio"
    "flag"
    "fmt"
    "io"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/watch"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "k8s.io/client-go/tools/clientcmd"
    "os"
    "path/filepath"
)

const (
    namespace = "test"
    outdir    = "out/logs"
)

func main() {
    fmt.Println("hello")
    client := createClent()
    if err := os.MkdirAll(outdir, 0777); err != nil {
        panic(err)
    }

    pods, err := client.CoreV1().Pods(namespace).Watch(metav1.ListOptions{})
    if err != nil {
        panic(err)
    }

    for podEvent := range pods.ResultChan() {
        fmt.Printf("%+v: ", podEvent.Type)
        if podEvent.Type == watch.Added || podEvent.Type == watch.Modified {
            pod, ok := podEvent.Object.(*corev1.Pod)
            if ok {
                for _, containerStatus := range pod.Status.ContainerStatuses {
                    if containerStatus.Ready {
                        go followContainerLogs(client, pod, containerStatus.Name)
                    }
                }
            }
        }
        fmt.Println()
    }
}

func followContainerLogs(client *kubernetes.Clientset, pod *corev1.Pod, containerName string) {
    stream, err := client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Follow: true, Container: containerName}).Stream()
    defer func() {
        if stream != nil {
            if err := stream.Close(); err != nil {
                fmt.Println(err)
            }
        }
    }()
    if err != nil {
        panic(err)
    }

    logFilename := constructLogFilename(pod, containerName)
    fmt.Printf("logging into [%s]\n", logFilename)
    logfile, err := os.Create(logFilename)
    defer logfile.Close()
    if err != nil {
        panic(err)
    }

    r := bufio.NewReader(stream)
    for {
        bytes, err := r.ReadBytes('\n')
        if _, err := logfile.Write(bytes); err != nil {
            panic(err)
        }

        if err != nil {
            if err != io.EOF {
                panic(err)
            }
            fmt.Println("end here")
            break
        }
    }
}

func constructLogFilename(pod *corev1.Pod, containerName string) string {
    return fmt.Sprintf("%s/%s_%s_%s_%s.log", outdir, namespace, pod.Name, pod.ObjectMeta.UID, containerName)
}

func createInClient() *kubernetes.Clientset {
    config, err := rest.InClusterConfig()
    if err != nil {
        panic(err.Error())
    }

    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        panic(err.Error())
    }

    return clientset
}

func createClent() *kubernetes.Clientset {
    var kubeconfig *string
    if home := homeDir(); home != "" {
        kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
    } else {
        kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
    }
    flag.Parse()

    // use the current context in kubeconfig
    config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
    if err != nil {
        panic(err.Error())
    }

    // create the clientset
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        panic(err.Error())
    }

    return clientset
}

func homeDir() string {
    if h := os.Getenv("HOME"); h != "" {
        return h
    }
    return os.Getenv("USERPROFILE") // windows
}
