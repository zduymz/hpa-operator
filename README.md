# hpa-operator

The `hpa-operator` will will create automatically Horizontal Pod Scaling base on `Annotations` in `spec.metadata` in `Deployment`. The annotations are
```
hpa.apixio.com/min -> minimum number of replicas
hpa.apixio.com/max -> maximum number of replicas 
hpa.apixio.com/template -> name of hpa template you want to use
```

### How it work
Simply checking Deployment periodically by using Deployment Informer and Lister. If deployment has the valid annotations, it will create or update HPA with the same name as Deployment Name.  
When Deployment is deleted, it also deleted either.  
HPA Templates were stored in ConfigMap that was mounted to `hpa-operator` as volume at `/template`
### Installing
HPA Template configmap
```bash
kubectl create -f https://raw.githubusercontent.com/zduymz/hpa-operator/master/examples/hpatemplate.yaml
```

HPA Operator with RBAC
```bash
kubectl create -f https://raw.githubusercontent.com/zduymz/hpa-operator/master/examples/hpa-rbac.yaml
```

HPA Operator without RBAC
```bash
kubectl create -f  https://raw.githubusercontent.com/zduymz/hpa-operator/master/examples/hpa.yaml
```

### Testing
Create nginx deployment
```bash
kubectl create -f https://raw.githubusercontent.com/zduymz/hpa-operator/master/examples/nginx.yaml
```

Send requests
```bash
kubectl run -it load-generator --image=busybox /bin/sh

while true; do wget -q -O- http://nginx-svc.default.svc.cluster.local; done
```

### Build yourself
You need to install golang > 1.12 and [dep]().  
```
# Install requirements package
dep ensure
# build on macos
make macos
# build on linux
make linux
```

### Note
We have some environment variables
* K8S_MASTER: k8s master url (automatically look up if empty)
* K8S_CONFIG: kubeconfig file (automatically look up if empty)
* HPA_TEMPLATES: directory contain hpa templates (default is /template/)