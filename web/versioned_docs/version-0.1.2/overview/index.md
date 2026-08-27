---
sidebar_position: 1
title: Introduction
slug: /
---

# Overview

ObjectStoreViewer is a read-only web application for inspecting PostgreSQL
backup repositories in object storage. You point it at exactly one repository
root — a Barman Cloud or pgBackRest layout in S3, Azure Blob Storage, or GCS —
and it scans that root in the background and publishes a bounded inventory to
the browser.

:::danger The one rule
ObjectStoreViewer reports **structural evidence**. It does not prove that a
restore will succeed. Absence of a detected problem is not proof of
restorability.
:::

That constraint is not modesty, it is the product. The application never reads
every byte, validates a checksum, decrypts an artifact, or runs PostgreSQL
recovery, so every conclusion it publishes carries its evidence scope,
freshness, and completeness — and stays `unknown` when any of those is missing.

## What it shows

- **Inventory** — exact object and stored-byte totals, but only after a
  complete scan; validated Barman server or pgBackRest stanza scopes; a bounded
  recent-object list; and refresh freshness.
- **Barman backup catalog** — completed, in-progress, failed, malformed,
  unsupported, and structurally incomplete backups, reported separately and
  never merged.
- **WAL evidence** — compact segment ranges plus candidate/confirmed gaps,
  partial, history, backup-history, duplicate, and unknown classifications,
  computed from the PostgreSQL version and segment size found in backup
  metadata.
- **Timelines and recovery coverage** — bounded timeline history connecting
  structurally usable backup anchors to per-timeline recovery paths, each
  stopping conservatively at missing ancestry, unknown evidence, a candidate or
  confirmed gap, or the observed archive frontier.

## The questions it answers

- Which backups are visible, completed, failed, in progress, or unreadable?
- Are the artifacts and dependency backups the configured format expects
  present?
- How much logical and stored data is represented?
- Which WAL segments and timelines are visible, and where are the known gaps?
- From which usable backup is there observed, contiguous recovery coverage?
- Is the archive fresh, and does its inventory match configured expectations?

It answers them without anyone extracting cloud credentials or running
format-specific repository commands by hand. CloudNativePG status reports what a
backup *operation* observed; the object store is independent evidence about which
artifacts are actually present.

## What it never does

It never writes. Domain code may use only list, bounded get/open, and stat/head
operations through a narrow read-store interface; a repository check fails the
build if a mutation-shaped identifier appears anywhere in the application
source. It also never authenticates a user, terminates TLS, downloads a raw
object (that is opt-in and [not yet built](../roadmap.md)), or claims "restore
verified", "restore guaranteed", "exact PITR window", or "restorable until".

## Non-goals

ObjectStoreViewer is not a generic object-store manager. It does not orchestrate
restores, invoke Barman or pgBackRest restore/mutation commands, connect to
PostgreSQL, write or delete objects, manage users, provide per-user or
multi-tenant authorization, inspect arbitrary bucket prefixes, verify entire
backup contents, or claim that a restore will succeed.

One deployment is one trust domain.

## Two runtimes, one binary

| Mode | Selected by | Surface |
|---|---|---|
| **standalone** | `RUNTIME_MODE` empty (default) | HTTP on `LISTEN_ADDR`: the inventory page `/`, the WAL browser `/wals`, `/healthz`, `/readyz` |
| **pgConsole sidecar** | `RUNTIME_MODE=pgconsole-sidecar` | no TCP listener at all — only the authenticated evidence API on a pod-private Unix socket |

The standalone runtime is what you deploy behind an authentication proxy. The
sidecar runtime is a producer for pgConsole; it is executable and proven at the
producer boundary, but no runtime pair is qualified or advertised as supported
yet.

## Where to go next

- [Concepts](concepts.md) — the trust boundary, the scan model, and how a
  conclusion gets its confidence.
- [The evidence model](evidence-model.md) — the status vocabulary and the
  claims each layer is allowed to make.
- [Getting started](../tutorials/getting-started.md) — a local repository and a
  first inventory in a few minutes.
- [Installation](../operations/installation.md) — the real deployment, with its
  proxy and network boundary.
