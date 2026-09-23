# Obstor Erasure Code Quickstart Guide

Obstor protects data against hardware failures and silent data corruption using erasure code and checksums. With the highest level of redundancy, you may lose up to half (N/2) of the total drives and still be able to recover the data.

## What is Erasure Code?

Erasure code is a mathematical algorithm to reconstruct missing or corrupted data. Obstor uses Reed-Solomon code to shard objects into variable data and parity blocks. For example, in a 12 drive setup, an object can be sharded to a variable number of data and parity blocks across all the drives - ranging from six data and six parity blocks to ten data and two parity blocks.

By default, Obstor shards the objects across N/2 data and N/2 parity drives. Though, you can use [storage classes](/docs/erasure/storage-class) to use a custom configuration. We recommend N/2 data and parity blocks, as it ensures the best protection from drive failures.

In 12 drive example above, with Obstor server running in the default configuration, you can lose any of the six drives and still reconstruct the data reliably from the remaining drives.

## Why is Erasure Code useful?

Erasure code protects data from multiple drives failure, unlike RAID or replication. For example, RAID6 can protect against two drive failure whereas in Obstor erasure code you can lose as many as half of drives and still the data remains safe. Further, Obstor's erasure code is at the object level and can heal one object at a time. For RAID, healing can be done only at the volume level which translates into high downtime. As Obstor encodes each object individually, it can heal objects incrementally. Storage servers once deployed should not require drive replacement or healing for the lifetime of the server. Obstor's erasure coded backend is designed for operational efficiency and takes full advantage of hardware acceleration whenever available.

![Erasure](https://raw.githubusercontent.com/obstor/obstor/main/docs/screenshots/erasure-code.svg)

## What is Bit Rot protection?

Bit Rot, also known as data rot or silent data corruption is a data loss issue faced by disk drives today. Data on the drive may silently get corrupted without signaling an error has occurred, making bit rot more dangerous than a permanent hard drive failure.

Obstor's erasure coded backend uses high speed [HighwayHash](https://github.com/minio/highwayhash) checksums to protect against Bit Rot.

## How are drives used for Erasure Code?

Obstor divides the drives you provide into erasure-coding sets of *4 to 16* drives.  Therefore, the number of drives you present must be a multiple of one of these numbers.  Each object is written to a single erasure-coding set.

Obstor uses the largest possible EC set size which divides into the number of drives given. For example, *18 drives* are configured as *2 sets of 9 drives*, and *24 drives* are configured as *2 sets of 12 drives*.  This is true for scenarios when running Obstor as a standalone erasure coded deployment. In [distributed setup however node (affinity) based](/docs/distributed) erasure stripe sizes are chosen.

The drives should all be of approximately the same size.

## Get Started with Obstor in Erasure Code

### 1. Prerequisites

Install Obstor - Obstor Quickstart Guide

### 2. Run Obstor Server with Erasure Code

Example: Start Obstor server in a 12 drives setup, using Obstor binary.

```sh
obstor server /data{1...12}
```

Example: Start Obstor server in a 8 drives setup, using Obstor Docker image.

```sh
docker run -p 9000:9000 --name obstor \
  -v /mnt/data1:/data1 \
  -v /mnt/data2:/data2 \
  -v /mnt/data3:/data3 \
  -v /mnt/data4:/data4 \
  -v /mnt/data5:/data5 \
  -v /mnt/data6:/data6 \
  -v /mnt/data7:/data7 \
  -v /mnt/data8:/data8 \
  ghcr.io/obstor/obstor server /data{1...8}
```

### 3. Test your setup

You may unplug drives randomly and continue to perform I/O on the system.
