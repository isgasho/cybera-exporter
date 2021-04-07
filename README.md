### Cybera Cloud site exporter for VictoriaMetrics


#### QuickStart

 Start `VictoriaMetrics` database:

```bash
./bin/victoria-metrics-prod
```

 Start exporter with given api url, username and password, pointing to your `VictoriaMetrics`
```bash
./bin/exporter -cybera.url=http://localhost:8013 -cybera.username "ff" -cybera.password="pass" --vm.url=http://localhost:8428
```

#### Configuration


  Important Configuration flags:
```bash
-cybera.scrapeConcurrency - how many requests execute concurently to cybera cloud api for retrieving information about site
-cybera.scrapeInterval - how often fetch information from cybera cloud api
-vm.pushInterval - how often push metrics to VictoriaMetricsDatabase
```