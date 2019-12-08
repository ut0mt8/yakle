# yakle
Yet Another Kafka Lag Exporter

Every kafka lag exporter are either broken, slow or only send metrics to influx.
Why not writing my own ? 

This is inspired by burrowx, but simplified with the more simple and robust logic I could think of.
It uses the kalag library written for the occasion. Kalag is fast cause it use only low level api.
The big plus compared to other exporter is that yakle reports not only offset lag but also the time lag, and not interpolated one.

## Exposed metrics

### Labels

**`topic`**: Topic name

**`partition`**: Partition ID 

**`group`**: Consumer group name


### Metrics

#### Consumer group metrics

| Metric | Description |
| --- | --- |
| `yakle_group_topic_partition_offset_lag{group, topic, partition}` | Number of messages the consumer group is behind for a given partition. |
| `yakle_group_topic_partition_time_lag{group, topic, partition}` |Time in millisecond the consumer group is behind for a given partition. |
| `yakle_group_topic_partition_current_offset{group, topic, partition}` | Current offset of a given group on a given partition. |

#### Topic / Partition metrics

| Metric | Description |
| --- | --- |
| `yakle_topic_partition_latest_offset{{topic, partition}` | Latest known commited offset for this partition. |


