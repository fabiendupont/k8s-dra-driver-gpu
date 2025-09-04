# NVIDIA DRA Driver for GPUs

Enables

* flexible and powerful allocation and dynamic reconfiguration of GPUs as well as
* allocation of ComputeDomains for robust and secure Multi-Node NVLink.

For Kubernetes 1.32 or newer, with Dynamic Resource Allocation (DRA) [enabled](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/#enabling-dynamic-resource-allocation).

## Overview

DRA is a novel concept in Kubernetes for flexibly requesting, configuring, and sharing specialized devices like GPUs.
To learn more about DRA in general, good starting points are: [Kubernetes docs](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/), [GKE docs](https://cloud.google.com/kubernetes-engine/docs/concepts/about-dynamic-resource-allocation), [Kubernetes blog](https://kubernetes.io/blog/2025/05/01/kubernetes-v1-33-dra-updates/).

Most importantly, DRA puts resource configuration and scheduling in the hands of 3rd-party vendors.

The NVIDIA DRA Driver for GPUs manages two types of resources: **GPUs** and **ComputeDomains**. Correspondingly, it contains two DRA kubelet plugins: [gpu-kubelet-plugin](https://github.com/NVIDIA/k8s-dra-driver-gpu/tree/main/cmd/gpu-kubelet-plugin), [compute-domain-kubelet-plugin](https://github.com/NVIDIA/k8s-dra-driver-gpu/tree/main/cmd/compute-domain-kubelet-plugin). Upon driver installation, each of these two parts can be enabled or disabled separately.

The two sections below provide a brief overview for each of these two parts of this DRA driver.

### `ComputeDomain`s

An abstraction for robust and secure Multi-Node NVLink (MNNVL). Officially supported.

An individual `ComputeDomain` (CD) guarantees MNNVL-reachability between pods that are _in_ the CD, and secure isolation from other pods that are _not in_ the CD.

In terms of placement, a CD follows the workload. In terms of lifetime, a CD is ephemeral: its lifetime is bound to the lifetime of the consuming workload.
For more background on how `ComputeDomain`s facilitate orchestrating MNNVL workloads on Kubernetes (and on NVIDIA GB200 systems in particular), see [this](https://docs.google.com/document/d/1PrdDofsPFVJuZvcv-vtlI9n2eAh-YVf_fRQLIVmDwVY/edit?tab=t.0#heading=h.qkogm924v5so) doc and [this](https://docs.google.com/presentation/d/1Xupr8IZVAjs5bNFKJnYaK0LE7QWETnJjkz6KOfLu87E/edit?pli=1&slide=id.g28ac369118f_0_1647#slide=id.g28ac369118f_0_1647) slide deck.
For an outlook and specific plans for improvements, please refer to [these](https://github.com/NVIDIA/k8s-dra-driver-gpu/releases/tag/v25.3.0-rc.3) release notes.

If you've heard about IMEX: this DRA driver orchestrates IMEX primitives (daemons, domains, channels) under the hood.

### `GPU`s

The GPU allocation side of this DRA driver [will enable powerful features](https://docs.google.com/document/d/1BNWqgx_SmZDi-va_V31v3DnuVwYnF2EmN7D-O_fB6Oo) (such as dynamic allocation of MIG devices).
To learn about what we're planning to build, please have a look at [these](https://github.com/NVIDIA/k8s-dra-driver-gpu/releases/tag/v25.3.0-rc.3) release notes.

While some GPU allocation features can be tried out, they are not yet officially supported.
Hence, the GPU kubelet plugin is currently disabled by default in the Helm chart installation.

For exploration and demonstration purposes, see the "demo" section below, and also browse the `demo/specs/quickstart` directory in this repository.

## 🔗 FabricManager Integration

### High-Bandwidth GPU Communication

The driver now supports **FabricManager integration** for optimal GPU-to-GPU communication through NVLink topology awareness. This enables high-bandwidth communication between GPUs instead of slower PCIe connections.

### Key Features

- **Virtualization Model-Aware**: Different allocation strategies for BareMetal, FullPassthrough, SharedNVSwitch, and vGPU
- **Resource Allocation Focus**: Allocates fabric resources rather than direct partition management
- **High-Bandwidth Communication**: Ensures GPUs communicate via NVLink with optimal topology
- **Backward Compatibility**: Maintains existing functionality with `FabricPartitionID` field

### Example Usage

```yaml
apiVersion: resource.k8s.io/v1alpha2
kind: ResourceClaim
metadata:
  name: gpu-claim-with-fabric
spec:
  resourceClassName: nvidia.com/gpu
  parameters:
    apiVersion: nvidia.com/v1beta1
    kind: GpuConfig
    sharing:
      strategy: TimeSlicing
    fabricTopologyConfig:
      strategy: Auto                    # Auto, Manual, or Disabled
      partitionSize: 4                  # 2, 4, or 8 GPUs
      virtualizationModel: BareMetal    # BareMetal, FullPassthrough, SharedNVSwitch, VGPU
      minBandwidth: 600.0              # GB/s
      maxLatency: 0.1                  # ms
      partitionIDs: ["partition-1"]    # For Manual strategy
```

### Virtualization Model Support

**BareMetal**:
- Direct partition management with full control
- GPUs allocated from optimal fabric partition
- High-bandwidth NVLink connectivity

**FullPassthrough**:
- Fabric resources allocated for guest VM management
- Guest VM handles its own partition management
- Optimal resource allocation within boundaries

**SharedNVSwitch**:
- DRA driver acts as service VM
- Manages partitions on behalf of guests
- Shared NVSwitch resources allocated

**vGPU**:
- Fabric topology optimization disabled
- Standard GPU allocation without fabric constraints

### Feature Gate

The FabricManager integration is controlled by the `FabricTopologySupport` feature gate:
- **Alpha**: Default disabled
- **Enable**: Set `FABRIC_TOPOLOGY_SUPPORT=true` in environment
- **Gradual Rollout**: Can be enabled per node or cluster-wide

## Installation

As of today, the recommended installation method is via Helm.
Detailed instructions can (for now) be found [here](https://github.com/NVIDIA/k8s-dra-driver-gpu/discussions/249).
In the future, this driver will be included in the [NVIDIA GPU Operator](https://github.com/NVIDIA/gpu-operator) and does not need to be installed separately anymore.

### Validating Admission Webhook

The validating admission webhook is disabled by default. To enable it, install cert-manager and its CRDs, then set the `webhook.enabled=true` value when the nvidia-dra-driver-gpu chart is installed.

```bash
helm install \
  --repo https://charts.jetstack.io \
  --version v1.16.3 \
  --create-namespace \
  --namespace cert-manager \
  --wait \
  --set crds.enabled=true \
  cert-manager \
  cert-manager
```

## A (kind) demo

Below, we demonstrate a basic use case: sharing a single GPU across two containers running in the same Kubernetes pod.

**Step 1: install dependencies**

Running this demo requires
* kind (follow the official [installation docs](https://kind.sigs.k8s.io/docs/user/quick-start/#installation))
* NVIDIA Container Toolkit & Runtime (follow a [previous version](https://github.com/NVIDIA/k8s-dra-driver-gpu/blob/5a4717f1ea613ad47bafccb467582bf2425f20f1/README.md#demo) of this readme for setup instructions)

**Step 2: create kind cluster with the DRA driver installed**

Start by cloning this repository, and `cd`in into it:

```console
git clone https://github.com/NVIDIA/k8s-dra-driver-gpu.git
cd k8s-dra-driver-gpu
```

Next up, build this driver's container image and create a kind-based Kubernetes cluster:

```console
export KIND_CLUSTER_NAME="kind-dra-1"
./demo/clusters/kind/build-dra-driver-gpu.sh
./demo/clusters/kind/create-cluster.sh
```

Now you can install the DRA driver's Helm chart into the Kubernetes cluster:

```console
./demo/clusters/kind/install-dra-driver-gpu.sh
```

**Step 3: run workload**

Submit workload:

```console
kubectl apply -f ./demo/specs/quickstart/gpu-test2.yaml
```

If you're curious, have a look at [the `ResourceClaimTemplate`](https://github.com/jgehrcke/k8s-dra-driver-gpu/blob/526130fbaa3c8f5b1f6dcfd9ef01c9bdd5c229fe/demo/specs/quickstart/gpu-test2.yaml#L12) definition in this spec, and how the corresponding _single_ `ResourceClaim` is [being referenced](https://github.com/jgehrcke/k8s-dra-driver-gpu/blob/526130fbaa3c8f5b1f6dcfd9ef01c9bdd5c229fe/demo/specs/quickstart/gpu-test2.yaml#L46) by both containers.

Container log inspection then indeed reveals that both containers operate on the same GPU device:

```bash
$ kubectl logs pod -n gpu-test2 --all-containers --prefix
[pod/pod/ctr0] GPU 0: NVIDIA A100-SXM4-40GB (UUID: GPU-4404041a-04cf-1ccf-9e70-f139a9b1e23c)
[pod/pod/ctr1] GPU 0: NVIDIA A100-SXM4-40GB (UUID: GPU-4404041a-04cf-1ccf-9e70-f139a9b1e23c)
```

## Contributing

Contributions require a Developer Certificate of Origin (DCO, see [CONTRIBUTING.md](https://github.com/NVIDIA/k8s-dra-driver-gpu/blob/main/CONTRIBUTING.md)).

## Support

Please open an issue on the GitHub project for questions and for reporting problems.
Your feedback is appreciated!
