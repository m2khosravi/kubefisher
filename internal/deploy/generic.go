package deploy

import (
	"context"
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
)

// GenericStrategy deploys a plain vLLM Deployment + Service when no serving platform is installed.
type GenericStrategy struct{}

func (GenericStrategy) Name() string { return "generic" }

func (GenericStrategy) PrimaryName(opts DeployOptions) string {
	return ModelSlug(opts.Model)
}

func (g GenericStrategy) Build(opts DeployOptions) ([]client.Object, error) {
	if err := validateDeployOptions(opts); err != nil {
		return nil, err
	}
	name := g.PrimaryName(opts)
	image := opts.Image
	if image == "" {
		image = DefaultVLLMImage
	}
	replicas := opts.Replicas
	if replicas < 1 {
		replicas = 1
	}
	team := opts.Team
	if team == "" {
		team = DefaultTeam
	}
	gpu := opts.GPU
	// Empty gpu means no node selector — do not fall back to a default here.
	// The default is applied at the CLI flag level (deploy_cmd.go).

	labels := map[string]string{
		"app.kubernetes.io/name": name,
		contract.AnnPlatform:     contract.PlatformVLLM,
		contract.LabelModel:      opts.Model,
		contract.LabelTeam:       team,
		contract.LabelWorkloadType: contract.WorkloadTypeInference,
	}

	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:  "vllm",
				Image: image,
				Args: []string{
					"python3",
					"-m",
					"vllm.entrypoints.openai.api_server",
					"--host", "0.0.0.0",
					"--port", "8000",
					"--model", opts.Model,
				},
				Ports: []corev1.ContainerPort{
					{Name: "http", ContainerPort: 8000},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
					},
					Limits: corev1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
					},
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromString("http")},
					},
					InitialDelaySeconds: 30,
					PeriodSeconds:       10,
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromString("http")},
					},
					InitialDelaySeconds: 60,
					PeriodSeconds:       20,
				},
			},
		},
	}
	if gpu != "" {
		podSpec.NodeSelector = map[string]string{"accelerator": gpu}
	}

	dep := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: opts.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: opts.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app.kubernetes.io/name": name},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8000, TargetPort: intstr.FromString("http")},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	return []client.Object{dep, svc}, nil
}

func (GenericStrategy) WaitReady(ctx context.Context, cl client.Client, _ dynamic.Interface, namespace, name string) (string, error) {
	var dep appsv1.Deployment
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &dep); err != nil {
		return "", fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}
	want := int32(1)
	if dep.Spec.Replicas != nil {
		want = *dep.Spec.Replicas
	}
	if dep.Status.AvailableReplicas < want {
		if msg, failed := checkSchedulingFailure(ctx, cl, namespace, name); failed {
			return "", fmt.Errorf("pod scheduling failed: %s\n\nTip: check node labels with: kubectl get nodes --show-labels", msg)
		}
		return "", fmt.Errorf("deployment not ready")
	}
	endpoint := fmt.Sprintf("http://%s.%s.svc:8000/v1/completions", name, namespace)
	return endpoint, nil
}

// checkSchedulingFailure lists pods for the deployment and inspects events for FailedScheduling.
// Returns the event message and true if a terminal scheduling failure is detected.
func checkSchedulingFailure(ctx context.Context, cl client.Client, namespace, appName string) (string, bool) {
	var podList corev1.PodList
	if err := cl.List(ctx, &podList,
		client.InNamespace(namespace),
		client.MatchingLabels{"app.kubernetes.io/name": appName},
	); err != nil {
		return "", false
	}
	for _, pod := range podList.Items {
		if pod.Status.Phase != corev1.PodPending {
			continue
		}
		var eventList corev1.EventList
		if err := cl.List(ctx, &eventList,
			client.InNamespace(namespace),
			&client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("involvedObject.name", pod.Name),
			},
		); err != nil {
			continue
		}
		for _, ev := range eventList.Items {
			if ev.Reason == "FailedScheduling" {
				return ev.Message, true
			}
		}
	}
	return "", false
}

func (GenericStrategy) ReadCostPerHour(ctx context.Context, cl client.Client, _ dynamic.Interface, namespace, name string) string {
	var dep appsv1.Deployment
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &dep); err != nil {
		return "—"
	}
	return formatCostAnnotation(dep.Annotations[contract.AnnCostPerHour])
}

func formatCostAnnotation(v string) string {
	if v == "" {
		return "—"
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return fmt.Sprintf("$%.2f/hr", f)
	}
	return v
}
