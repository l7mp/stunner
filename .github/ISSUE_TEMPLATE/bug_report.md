---
name: Bug report
about: Create a report to help us improve STUNner
title: ''
labels: ''
assignees: ''

---

### Description

[Description of the problem]

### Steps to Reproduce

[Brief description of the steps you took to encounter the problem, if applicable]

**Expected behavior:** [What you expected to happen]

**Actual behavior:** [What actually happened]

### Versions

[Which version of STUNner you are using]

### Info

[Please copy-paste the output of the below commands and make sure to remove all sensitive information, like usernames, passwords, IP addresses, etc.]

#### Gateway API status

[Output of `kubectl get gateways,gatewayconfigs,gatewayclasses,udproutes.stunner.l7mp.io --all-namespaces -o yaml`]

#### Operator logs 

[Output of `kubectl -n stunner-system logs $(kubectl get pods -l control-plane=stunner-gateway-operator-controller-manager --all-namespaces -o jsonpath='{.items[0].metadata.name}')`]
