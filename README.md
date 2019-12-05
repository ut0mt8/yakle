# yakle
Yet Another Kafka Lag Exporter

Every kafka lag exporter are either broken, slow or only send metrics to influx.

Why not writing my own ? 

This one is heavily inspired by burrowx, but simplified with the more robust logic I could think of.
It uses the kalag library written for the occasion that is fast.
The another big difference compared to other exporter is that yakle also reports not only the offset lag but also the time lag.


