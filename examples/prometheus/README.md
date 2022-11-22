# Monitoring STUNner with Prometheus and Grafana

This tutorial demonstrates monitoring of a STUNner instance with [Prometheus](https://prometheus.io/) and [Grafana](https://grafana.com/).

In this demo you will learn how to:
* monitor STUNner with [Prometheus](https://prometheus.io/)
* how to visualize STUNner metrics using [Grafana](https://grafana.com/)

## Installation

### Prerequisites

* The tutorial assumes a fresh STUNner installation in a namespace called `stunner`; see the [STUNner installation and configuration guide](/doc/INSTALL.md).
* You need a web browser to run this tutorial.

### Enable STUNner metrics endpoint

After installing STUNner, enable the STUNner metrics endpoint:
```console
kubectl -n stunner patch gatewayconfigs.stunner.l7mp.io stunner-gatewayconfig --patch '{"spec": {"metricsEndpoint": "http://0.0.0.0:8080/metrics" }}' --type=merge
```

Patch STUNner deployment to enable the monitoring port (required by the pod monitor):
```console
kubectl -n stunner patch deployment stunner --patch '{"spec":{"template":{"spec":{"containers":[{"name":"stunnerd","ports":[{"name": "web","containerPort": 8080 }]}]}}}}'
```

### Deploy Prometheus and Grafana

Deploy Prometheus with `helm`:
```console
helm install prometheus stunner/stunner-prometheus
```

The helm chart creates the namespace `monitoring` and installs Prometheus along with the prometheus-operator, and Grafana.

## Usage

### Observe metrics on the Prometheus dashboard (optional)
By default Prometheus dashboard is not accessable outside of the cluster. No worries if that is the case -- you can still observe the metrics with Grafana.

In certain deployments (e.g., local minikube) you can open the Prometheus dashboard by navigating your browser to prometheus service IP and port `9090`.  For example, if the prometheus service cluster-IP is `10.103.67.2`, navigate to `http://10.103.67.2:9090`.

To get the service IP, use this command: `kubectl get svc -n monitoring prometheus -o custom-columns=:.spec.clusterIP --no-headers`; to have a clickable link: `echo -n "http://$(kubectl get svc -n monitoring prometheus -o custom-columns=:.spec.clusterIP --no-headers):9090"`

If everything goes well, the Prometheus dashboard opens:
TODO add img

Next, we observe the `stunner_allocations_active` metrics:

1. Write `stunner_allocations_active` to the marked field (next to the looking glass icon)
2. Click on the `Execute` button
3. Switch to `Graph` view tab.

![Prometheus Dashboard](prometheus-dashboard.png)

### Setup Grafana dashboard

We use Grafana to visualize STUNner metrics.

To get the Grafana service IP, use this command: `kubectl get svc -n monitoring grafana -o custom-columns=:.spec.clusterIP --no-headers`; to have a clickable link: `echo -n "http://$(kubectl get svc -n monitoring grafana -o custom-columns=:.spec.clusterIP --no-headers)"`

#### 1. Login

The default username is **admin**.

The password is **admin**.

At the first login you can change the password or leave as it is (use the Skip button).

#### 2. Check Prometheus data source (optional)

The helm chart configures Prometheus as a data source. You can double check the data source at any time.

To do this click on *Configuration* (1), then *Data sources* (2), as shown here:

![grafana datasource check step 1](grafana-prom-datasource_0.png)

This will open up the datasources page. At the bottom of the page there is button *Save & test*. Click on it and observe the datasource is working (2):

![grafana datasource check step 2](grafana-prom-datasource_1.png)

#### 3. Visualize a STUNner metric

Next, we plot a STUNner metric. For this purpose, we create a new panel presenting the active allocations metric.

Click on Add panel (1), then Add new panel (2):

![Grafana Add new panel](grafana-add-panel-dashboard_0.png)

This will open a panel configuration window.

1. Set the datasource: prometheus
2. Choose a metric. In this example we use `stunner_allocations_active`.
3. Click on *Run queries* (this will update the fig)
4. Fine-tune plot parameters. In this example, we set the title.
5. Click *Apply*

![Grafana Panel Configuration](grafana-add-panel-config_0.png)

Finally, check the new panel showing the `stunner_allocations_active` metric:

![Grafana Dashboard with the new panel](grafana-add-panel-dashboard_1.png)

This ends the Prometheus/Grafana example.

### Generate allocations (optional)

To observe metric changes, generate STUNner connections and thus modify the `stunner_allocations_active` metric. For this purpose, we recommend [our examples](https://github.com/l7mp/stunner/tree/main/examples).

## Troubleshooting

| Problem | Solution |
| :--- | :--- |
|Cannot access prometheus and grafana services in minikube | Run `minikube tunnel` |
|Pod monitor not showing up in Prometheus | Patch stunner deployment to open monitoring port + enable stunner monitoring |

## Clean up

Uninstall prometheus with `helm`:
```console
helm uninstall prometheus
```

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.

## Acknowledgments

Prometheus helm chart is based on the [l7mp-prometheus chart](https://github.com/l7mp/l7mp/tree/master/helm-charts/l7mp-prometheus).
