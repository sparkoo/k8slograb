package main

import (
    "bufio"
    "bytes"
    "flag"
    "fmt"
    "io"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/apimachinery/pkg/watch"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/kubernetes/scheme"
    "k8s.io/client-go/rest"
    "k8s.io/client-go/tools/clientcmd"
    "k8s.io/client-go/tools/remotecommand"
    "log"
    "os"
    "path/filepath"
)

const (
    namespace        = "che-che"
    outdir           = "out/workspace_logs"
    workspaceIdLabel = "che.workspace_id"
)

func main() {
    log.Println("hello")
    config := createOutConfig()
    //client := createInClient()
    client := createClent(config)
    if err := os.MkdirAll(outdir, 0777); err != nil {
        log.Fatal(err)
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
                            if err := followContainerLogs(client, config, followers, pod, containerName); err != nil {
                                log.Printf("failed when following the logs of [%s][%s]", pod.Name, containerStatus.Name)
                            }
                        }(pod, containerStatus.Name)
                    }
                }
            }
        }
    }
}

func followContainerLogs(client *kubernetes.Clientset, config *rest.Config, followers map[string]bool, pod *corev1.Pod, containerName string) error {

    if err := grabFilelog(client, config, namespace, pod, containerName); err != nil {
        log.Fatal(err)
    }

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
        buffer, err := r.ReadBytes('\n')
        if _, err := logfile.Write(buffer); err != nil {
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

func grabFilelog(client *kubernetes.Clientset, config *rest.Config, namespace string, pod *corev1.Pod, containerName string) error {

    req := client.CoreV1().RESTClient().Post().Resource("pods").
        Namespace(namespace).
        Name(pod.Name).
        SubResource("exec").
        Param("container", containerName)
    req.VersionedParams(&corev1.PodExecOptions{
        Stdin:     false,
        Stdout:    true,
        Stderr:    true,
        TTY:       true,
        Container: containerName,
        Command:   []string{"tail", "/workspace_logs/meh"},
    }, scheme.ParameterCodec)

    log.Printf("request [%+v]\n", req)

    exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
    if err != nil {
        return err
    }

    log.Printf("exec [%+v]\n", exec)

    var stdout, stderr bytes.Buffer
    err = exec.Stream(remotecommand.StreamOptions{
        Stdin:  nil,
        Stdout: &stdout,
        Stderr: &stderr,
        Tty:    true,
    })
    if err != nil {
        return err
    }

    log.Println("execed ? ")

    r := bufio.NewReader(&stdout)
    for {
        buffer, err := r.ReadBytes('\n')
        log.Print(string(buffer))

        if err != nil {
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

func createInConfig() *rest.Config {
    config, err := rest.InClusterConfig()
    if err != nil {
        log.Fatal(err)
    }

    return config
}

func createOutConfig() *rest.Config {
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
        log.Fatal(err)
    }
    return config
}

func createClent(config *rest.Config) *kubernetes.Clientset {
    // create the clientset
    config.GroupVersion = &schema.GroupVersion{
        Group:   "",
        Version: "v1",
    }
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        log.Fatal(err)
    }

    return clientset
}

func homeDir() string {
    if h := os.Getenv("HOME"); h != "" {
        return h
    }
    return os.Getenv("USERPROFILE") // windows
}
