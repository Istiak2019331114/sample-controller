

---

# Creating a Sample Kubernetes Controller

## Step 1: Define the API

Create your API definition under the following directory structure:

```
pkg/api/<groupname>/<version>/
```

Inside this folder:

* **`types.go`** — Define the Go structs for your Custom Resource (CR).
* **`doc.go`** — Add package-level documentation and API group metadata.
* **`register.go`** — Register your types with the scheme.

---

## Step 2: Generate Code

Use the Kubernetes code generators to produce deepcopy functions, clientsets, listers, and informers.

1. Ensure you have boilerplate files like `boilerplate.go.txt`, `tools.go`, and a script `update-codegen.sh`.
2. Run the script:

   ```bash
   bash hack/update-codegen.sh
   ```

This will generate all required client and deepcopy code from your API definitions.

---

## Step 3: Generate the CRD Manifest

Use `controller-gen` to generate the CustomResourceDefinition (CRD) manifests.

1. Install `controller-gen`:

   ```bash
   go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
   ```

2. Generate CRD YAMLs and output them to the manifests directory:

   ```bash
   go run sigs.k8s.io/controller-tools/cmd/controller-gen crd paths=./pkg/api/... output:crd:dir=./manifests
   ```

---

## Step 4: Write the Controller Logic

* Implement the reconciliation loop for your controller.
* Define event handlers and business logic to act on changes to your Custom Resources.

---

## Step 5: Wire Up the Controller

In `main.go`:

* Configure the Kubernetes client and clientset.
* Create shared informer factories.
* Initialize your controller with the informers and logic.
* Register the controller with the controller-runtime manager.

---

## Step 6: Deploy and Test

### 1. Apply the CRD

```bash
kubectl apply -f manifests/
```

### 2. Run the Controller

Run locally for development:

```bash
go run main.go
```

Or build and deploy inside a container:

```bash
docker build -t my-controller .
kubectl apply -f deploy/controller.yaml
```

### 3. Create a Custom Resource (CR)

`sample-cr.yaml`:

```yaml
apiVersion: <groupname>/<version>
kind: <Kind>
metadata:
  name: sample-cr
spec:
  foo: bar
```

Apply it:

```bash
kubectl apply -f sample-cr.yaml
```

### 4. Verify

```bash
kubectl get <plural-kind>
kubectl describe <kind> sample-cr
```

Check controller logs to confirm reconciliation.

---

## Step 7: Example Test Scenarios

### Scenario 1: Create a Valid CR

**Action:** Apply `sample-cr.yaml`.
**Expectation:** Controller picks it up, reconciliation logic runs, and desired state is applied (for example, a Deployment or ConfigMap gets created).

---

### Scenario 2: Apply an Invalid CR

`invalid-cr.yaml`:

```yaml
apiVersion: <groupname>/<version>
kind: <Kind>
metadata:
  name: invalid-cr
spec:
  foo: 123   # Suppose `foo` expects a string
```

**Expectation:**

* If schema validation fails, Kubernetes rejects the object.
* If schema passes but value is invalid for your business logic, the controller should log an error or update `status.conditions` to reflect the issue.

---

### Scenario 3: Update the CR

**Action:**

```bash
kubectl edit <kind> sample-cr
```

Change `foo: bar` to `foo: baz`.

**Expectation:**

* Controller detects the change.
* Reconciliation logic updates dependent resources accordingly.

---

### Scenario 4: Delete the CR

**Action:**

```bash
kubectl delete <kind> sample-cr
```

**Expectation:**

* Controller receives the delete event.
* Associated resources are cleaned up, unless finalizers are in use.

---

### Scenario 5: Finalizer Handling

**Action:** Add a finalizer to the CR in your controller.

**Expectation:**

* When the CR is deleted, it remains in `Terminating` until the controller performs cleanup.
* Once cleanup is complete, the finalizer is removed and deletion finishes.

---

