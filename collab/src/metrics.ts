// Prometheus metrics (Decision 27), served at GET /metrics on the same HTTP
// port as the WebSocket endpoint (see app.ts). Optional OTLP tracing is not
// implemented.
import { Counter, Gauge, Histogram, Registry, collectDefaultMetrics } from 'prom-client'

export const registry = new Registry()
collectDefaultMetrics({ register: registry, prefix: 'collab_' })

export const connectionsGauge = new Gauge({
  name: 'collab_connections',
  help: 'Open document connections (a client with N documents counts N)',
  registers: [registry],
})
export const documentsGauge = new Gauge({
  name: 'collab_documents',
  help: 'Documents currently loaded in memory on this replica',
  registers: [registry],
})
export const storeFailures = new Counter({
  name: 'collab_store_failures_total',
  help: 'Failed document store attempts (each retry counts)',
  registers: [registry],
})
export const storeLatency = new Histogram({
  name: 'collab_store_duration_seconds',
  help: 'Latency of a successful document store (Postgres snapshot + write-back)',
  buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5],
  registers: [registry],
})
export const writebackFailures = new Counter({
  name: 'collab_writeback_failures_total',
  help: 'Stores whose Yjs snapshot was saved but pages.content could not be derived/written',
  registers: [registry],
})
export const externalReplacements = new Counter({
  name: 'collab_external_content_replacements_total',
  help: 'Times a manual write to pages.content replaced the live document state',
  registers: [registry],
})
export const accessRevocations = new Counter({
  name: 'collab_access_revocations_total',
  help: 'Connections closed because access was revoked or the token expired',
  registers: [registry],
})
