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
    "log"
    "os"
    "path/filepath"
)

const (
    namespace        = "che-che"
    outdir           = "/workspace_logs"
    workspaceIdLabel = "che.workspace_id"
)

func main() {
    log.Println("hello")
    client := createInClient()
    if err := os.MkdirAll(outdir, 0777); err != nil {
        panic(err)
    }
    pods, err := client.CoreV1().Pods(namespace).Watch(metav1.ListOptions{LabelSelector: workspaceIdLabel})
    if err != nil {
        log.Fatal(err)
    }

    followers := make(map[string]bool)

    for podEvent := range pods.ResultChan() {
        if podEvent.Type == watch.Added || podEvent.Type == watch.Modified {
            pod, ok := podEvent.Object.(*corev1.Pod)
            if ok {
                for _, containerStatus := range pod.Status.ContainerStatuses {
                    if containerStatus.Ready {
                        go func(pod *corev1.Pod, containerName string) {
                            if err := followContainerLogs(client, followers, pod, containerName); err != nil {
                                log.Printf("failed when following the logs of [%s][%s]", pod.Name, containerStatus.Name)
                            }
                        }(pod, containerStatus.Name)
                    }
                }
            }
        }
    }
}

func followContainerLogs(client *kubernetes.Clientset, followers map[string]bool, pod *corev1.Pod, containerName string) error {
    logFilename := constructLogFilename(pod, containerName)
    if _, follows := followers[logFilename]; follows {
        log.Printf("Already following [%s]\n", logFilename)
        return nil
    }

    stream, err := client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Follow: true, Container: containerName}).Stream()
    followers[logFilename] = true
    defer func() {
        clean(followers, logFilename)
        if stream != nil {
            if err := stream.Close(); err != nil {
                log.Println(err)
            }
        }
    }()
    if err != nil {
        clean(followers, logFilename)
        return err
    }

    log.Printf("logging pod [%s] container [%s] into [%s]\n", pod.Name, containerName, logFilename)
    logfile, err := os.Create(logFilename)
    defer func() {
        clean(followers, logFilename)
        if logfile != nil {
            if err := logfile.Close(); err != nil {
                log.Printf("unable to close logfile [%s]\n", err.Error())
            }
        }
    }()
    if err != nil {
        clean(followers, logFilename)
        return err
    }

    r := bufio.NewReader(stream)
    for {
        bytes, err := r.ReadBytes('\n')
        if _, err := logfile.Write(bytes); err != nil {
            clean(followers, logFilename)
            return err
        }

        if err != nil {
            clean(followers, logFilename)
            if err != io.EOF {
                return err
            }
            log.Printf("end of log pod [%s] container [%s]\n", pod.Name, containerName)
            break
        }
    }

    return nil
}

func clean(followers map[string]bool, filename string) {
    log.Printf("Cleaning follower [%s] ... ", filename)
    if _, ok := followers[filename]; ok {
        log.Print("done\n")
        delete(followers, filename)
    } else {
        log.Print("no follower foud\n")
    }
}

func constructLogFilename(pod *corev1.Pod, containerName string) string {
    return fmt.Sprintf("%s/%s/%s/%s.log", outdir, pod.Labels[workspaceIdLabel], "che-logs-che-workspace-pod", containerName)
}

func createInClient() *kubernetes.Clientset {
    config, err := rest.InClusterConfig()
    if err != nil {
        panic(err)
    }

    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        panic(err)
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
        panic(err)
    }

    // create the clientset
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        panic(err)
    }

    return clientset
}

func homeDir() string {
    if h := os.Getenv("HOME"); h != "" {
        return h
    }
    return os.Getenv("USERPROFILE") // windows
}
