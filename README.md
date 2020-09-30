# SMP Exporter

## Build

```
go build
```

## Prometheus Configuration

Example config:
```yml
scrape_configs:
  - job_name: "smp"
    metrics_path: /probe
    basic_auth:
      username: user
      password: CHANGE_ME
    static_configs:
      - labels:
          device: smp351
        targets:
        - https://SMP_URL
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - source_labels: [instance]
        regex: .*\/\/([^\.]*)
        replacement: $1
        target_label: instance
      - target_label: __address__
        replacement: 127.0.0.1:9109
```
