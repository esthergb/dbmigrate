# dbmigrate — Reporte de Estado del Proyecto

**Fecha:** 2026-03-12
**Revisión de código:** commit `8e29ae5` (main, PR #85 merged)
**Build:** ✅ Compila correctamente
**Tests unitarios:** ✅ 12/12 paquetes pasan (`go test ./...`)
**go vet:** ✅ Sin problemas

---

## 1. Resumen Ejecutivo

El proyecto `dbmigrate` es un CLI en Go para migración de bases de datos MySQL/MariaDB con replicación incremental y verificación de consistencia. Se encuentra en una **fase avanzada de desarrollo pre-release v1**, con la mayoría de la funcionalidad core implementada y endurecida a través de 85 PRs. Sin embargo, hay funcionalidades significativas definidas en `Instructions.md` que quedan pendientes para v2/v3.

### Estado general: ~70% de la especificación completa implementada

- **Funcionalidad core (migrate/replicate/verify/plan/report):** Implementada y funcional
- **Prechecks y validaciones de compatibilidad:** Muy completos (15+ prechecks)
- **Replicación binlog:** Implementada con checkpoints y manejo de conflictos
- **Verificación de datos:** 4 modos implementados (count/hash/sample/full-hash)
- **Replicación trigger-based CDC:** NO implementada (reservada v2)
- **Replicación híbrida:** NO implementada (reservada v2)
- **GTID start:** NO implementado (reservado v2)
- **Routines/triggers/events migration:** NO implementados (reservados v2)
- **Reporte HTML:** NO implementado
- **User/grant migration:** NO implementado
- **Concurrencia real en migración de datos:** NO implementada

---

## 2. Lo que ESTÁ Hecho y Funcional

### 2.1 Infraestructura del Proyecto ✅

| Componente | Estado | Archivos |
| --- | --- | --- |
| Repositorio Git + GitHub | ✅ Completo | `.git/`, remote configurado |
| CI/CD (GitHub Actions) | ✅ Completo | `.github/workflows/ci.yml` — build, fmt, test, lint, vulncheck, smoke integration |
| PR template | ✅ Completo | `.github/pull_request_template.md` |
| Contributing guide | ✅ Completo | `CONTRIBUTING.md` |
| License (MIT) | ✅ Completo | `LICENSE` |
| Makefile | ✅ Completo | build, fmt, lint, test, vulncheck, release gates |
| Docker Compose | ✅ Completo | 14 servicios: MariaDB 10.6/10.11/11.0/11.4/11.8/12.0, MySQL 8.0/8.4 (pares a/b) |
| Datasets de prueba | ✅ Completo | 11 archivos SQL con datos deterministas |
| Config YAML files | ✅ Completo | 30 configuraciones para distintas combinaciones de migración |
| Scripts de test | ✅ Completo | 46 scripts de test para la matriz completa |

### 2.2 CLI y Subcomandos ✅

Los 5 subcomandos definidos en `Instructions.md` están implementados:

| Subcomando | Estado | Descripción |
| --- | --- | --- |
| `plan` | ✅ Funcional | Precheck de compatibilidad con 15+ validadores |
| `migrate` | ✅ Funcional | Migración baseline de schema + datos |
| `replicate` | ⚠️ Parcial | Solo modo binlog; trigger-CDC y hybrid reservados para v2 |
| `verify` | ✅ Funcional | Schema y datos (count/hash/sample/full-hash) |
| `report` | ✅ Funcional | Agregación de artefactos del state-dir |

### 2.3 Flags Globales Implementados

| Flag | Estado |
| --- | --- |
| `--source` | ✅ |
| `--dest` | ✅ |
| `--databases` | ✅ |
| `--exclude-databases` | ✅ |
| `--include-objects` | ⚠️ Solo `tables,views`; routines/triggers/events reservados v2 |
| `--concurrency` | ⚠️ Parseado pero no usado en migración de datos real |
| `--dry-run` | ✅ Con modo sandbox |
| `--dry-run-mode` (plan/sandbox) | ✅ |
| `--verbose` | ⚠️ Parseado, logging estructurado no implementado |
| `--json` | ✅ |
| `--tls-mode` | ✅ (default: required) |
| `--ca-file`, `--cert-file`, `--key-file` | ✅ |
| `--state-dir` | ✅ |
| `--config` (YAML file) | ✅ Con precedencia CLI > file |
| `--operation-timeout` | ✅ |
| `--downgrade-profile` | ✅ (strict-lts/same-major/adjacent-minor/max-compat) |

### 2.4 Flags de Migración

| Flag | Estado |
| --- | --- |
| `--schema-only` | ✅ |
| `--data-only` | ✅ |
| `--chunk-size` | ✅ (default 1000) |
| `--resume` | ✅ Con checkpoint |
| `--dest-empty-required` | ✅ |
| `--force` | ✅ |
| `--consistent-snapshot` | ✅ Implementado internamente (REPEATABLE READ + START TRANSACTION WITH CONSISTENT SNAPSHOT) |
| `--lock-tables` | ❌ No implementado |

### 2.5 Flags de Replicación

| Flag | Estado |
| --- | --- |
| `--replication-mode` | ⚠️ Solo `binlog`; capture-triggers/hybrid fail-fast |
| `--start-from` | ⚠️ Solo `auto` y `binlog-file:pos`; GTID reservado v2 |
| `--apply-ddl` (ignore/apply/warn) | ✅ |
| `--max-events` | ✅ |
| `--max-lag-seconds` | ✅ |
| `--conflict-policy` (fail/source-wins/dest-wins) | ✅ |
| `--conflict-values` (redacted/plain) | ✅ |
| `--source-server-id` | ✅ |
| `--enable-trigger-cdc` | ❌ Reservado v2 (fail-fast) |
| `--teardown-cdc` | ❌ Reservado v2 (fail-fast) |
| `--idempotent` | ❌ Reservado v2 (fail-fast) |
| `--start-file`, `--start-pos` | ✅ |

### 2.6 Flags de Verificación

| Flag | Estado |
| --- | --- |
| `--verify-level` (schema/data) | ✅ (sin nivel `full` combinado) |
| `--data-mode` (count/hash/sample/full-hash) | ✅ Todos implementados |
| `--sample-size` | ✅ |
| `--hash-alg` | ❌ No implementado (usa SHA-256 fijo) |
| `--row-hash-mode` | ❌ No implementado como flag explícito |
| `--tolerate-collation-diffs` | ❌ No implementado |
| `--ignore-definer-diffs` | ⚠️ Normalización automática, sin flag explícito |
| `--ignore-auto-inc` | ⚠️ Normalización automática, sin flag explícito |
| `--ignore-table-options` | ❌ No implementado |

### 2.7 Prechecks de Plan/Migrate (Muy Completos) ✅

| Precheck | Implementado |
| --- | --- |
| Zero-date defaults | ✅ Con generación de scripts ALTER |
| Plugin lifecycle (auth + storage engines) | ✅ |
| Invisible columns / GIPK | ✅ |
| Collation compatibility | ✅ Con artefactos persistentes |
| Schema features (SEQUENCE, SYSTEM VERSIONING, JSON cross-engine) | ✅ |
| Identifier portability (reserved words) | ✅ |
| Parser drift (SQL modes: ANSI_QUOTES, PIPES_AS_CONCAT, etc.) | ✅ |
| `lower_case_table_names` portability | ✅ |
| Foreign key cycles | ✅ |
| Replication boundary (GTID/binlog) | ✅ |
| Replication readiness (log_bin, binlog_format, binlog_row_image) | ✅ |
| Timezone portability | ✅ |
| Data shape (keyless tables, representation-sensitive) | ✅ |
| Manual evidence findings | ✅ |

### 2.8 Paquetes Internos

| Paquete | Líneas aprox. | Estado | Tests |
| --- | --- | --- | --- |
| `internal/cli` | ~210 | ✅ | ✅ 17K test |
| `internal/commands` | ~4000+ | ✅ | ✅ 14 test files |
| `internal/compat` | ~640 | ✅ | ✅ 13K test |
| `internal/config` | ~330 | ✅ | ✅ |
| `internal/data` | ~950 | ✅ | ✅ |
| `internal/db` | ~200 | ✅ | ✅ |
| `internal/replicate/binlog` | ~2900 | ✅ | ✅ 47K test |
| `internal/schema` | ~610 | ✅ | ✅ |
| `internal/state` | ~650 | ✅ | ✅ |
| `internal/verify/schema` | ~400 | ✅ | ✅ |
| `internal/verify/data` | ~1300 | ✅ | ✅ |
| `internal/version` | ~10 | ✅ | No test (trivial) |

### 2.9 Documentación

| Documento | Estado | Tamaño |
| --- | --- | --- |
| `README.md` | ✅ Extenso y alineado | 22.9KB |
| `docs/known-problems.md` | ✅ Completo | 26.3KB |
| `docs/risk-checklist.md` | ✅ Completo | 8.4KB |
| `docs/development-plan.md` | ✅ | 5.1KB |
| `docs/operators-guide.md` | ✅ Extenso | 31.8KB |
| `docs/security.md` | ✅ Básico | 1.3KB |
| `docs/v1-release-criteria.md` | ✅ | 7.0KB |
| `docs/v1-release-plan.md` | ✅ | 8.7KB |
| `docs/v1-release-decision.md` | ✅ | 6.1KB |
| `docs/v1-matrix-evidence.md` | ✅ | 5.9KB |
| `docs/v1-rehearsal-evidence.md` | ✅ | 7.3KB |
| `docs/version-compatibility-research.md` | ✅ | 5.5KB |
| `CONTINUITY.md` | ✅ Actualizado | 9.4KB |

---

## 3. Lo que ESTÁ Verificado (con evidencia)

### 3.1 Build y CI

- ✅ Binary compila (`go build -trimpath`)
- ✅ Todos los unit tests pasan (12 paquetes)
- ✅ `go vet` limpio
- ✅ `golangci-lint` limpio (verificado en PR #85)
- ✅ CI workflow configurado con build + fmt + test + lint + vulncheck + smoke integration

### 3.2 Matriz v1 Validada (Docker)

Los scripts de test en `scripts/` cubren estas combinaciones verificadas:

**Core v1 (strict-lts):**

- MySQL 8.4 → MySQL 8.4
- MariaDB 10.11 → MariaDB 10.11
- MariaDB 11.4 → MariaDB 11.4
- MariaDB 11.8 → MariaDB 11.8
- MariaDB 11.4 → MySQL 8.4
- MySQL 8.4 → MariaDB 11.4

**Supplemental v1:**

- MariaDB 10.11 → MariaDB 11.4
- MariaDB 10.11 → MariaDB 11.8
- MariaDB 11.4 → MariaDB 11.8
- MySQL 8.0 → MySQL 8.4

### 3.3 Rehearsals Documentados

Se ejecutaron rehearsals enfocados para:

- Metadata lock scenario
- Backup/restore
- Timezone
- Plugin lifecycle
- Replication transaction shape
- Invisible/GIPK
- Collation
- Verify canonicalization

---

## 4. Lo que se Puede Usar en Producción (v1 scope)

**Con las restricciones documentadas**, los siguientes flujos están listos para uso en producción:

### 4.1 Flujo Baseline Migration

```bash
dbmigrate plan --source ... --dest ... --config ...
dbmigrate migrate --source ... --dest ... --config ...
dbmigrate verify --source ... --dest ... --verify-level=schema
dbmigrate verify --source ... --dest ... --verify-level=data --data-mode=count
dbmigrate report --state-dir ./state
```

**Restricciones:**

- Solo para pares de la matriz v1 frozen (MySQL 8.4, MariaDB 10.11/11.4/11.8)
- Solo `tables` y `views` como include-objects
- Requiere binlog_format=ROW, binlog_row_image=FULL en source
- No migra routines, triggers, events, users/grants
- No soporta tablas con FK cycles
- No soporta tablas sin primary key en replicación

### 4.2 Flujo Incremental Replication

```bash
dbmigrate replicate --source ... --dest ... --config ... --apply-ddl=warn
```

**Restricciones:**

- Solo modo binlog (no trigger-CDC, no hybrid)
- Requiere permisos REPLICATION SLAVE + REPLICATION CLIENT
- DDL mixing con row events causa fail-fast (diseñado así)
- No soporta GTID como start-from
- Tablas sin PK bloquean replay de UPDATE/DELETE

### 4.3 Flujo de Verificación

Los 4 modos de verificación de datos funcionan:

- `count`: conteo de filas
- `hash`: hash chunked con PK ordering
- `sample`: muestreo determinista
- `full-hash`: hash completo de tabla

---

## 5. Lo que QUEDA por Hacer

### 5.1 Funcionalidades NO Implementadas (de Instructions.md)

#### Prioridad ALTA (necesarias para producto completo)

| Funcionalidad | Sección Instructions.md | Estado | Complejidad |
| --- | --- | --- | --- |
| **Trigger-based CDC** (fallback) | §4 | ❌ No implementada | Alta |
| **Hybrid replication mode** | §4 | ❌ No implementada | Alta |
| **GTID support** (start-from=gtid) | §4 | ❌ No implementada | Media |
| **Routines migration** (procedures/functions) | §2, §5 | ❌ No implementada | Media |
| **Triggers migration** | §2, §5 | ❌ No implementada | Media |
| **Events migration** | §2, §5 | ❌ No implementada | Media |
| **User/grant migration** | §0.4 | ❌ No implementada | Media |
| **verify-level=full** (schema + data combinado) | §5 | ❌ No implementada | Baja |
| **Concurrencia real** en data copy | §7 | ❌ `--concurrency` parseado pero no usado | Media |
| **Structured logging** (JSON) | §7 | ❌ Solo fmt.Fprintf | Media |

#### Prioridad MEDIA

| Funcionalidad | Sección | Estado | Complejidad |
| --- | --- | --- | --- |
| **HTML report** | §3 report | ❌ Solo JSON/text | Media |
| **`--lock-tables` fallback** | §3 migrate | ❌ | Baja |
| **`--hash-alg` selectable** (xxhash64/sha256) | §3 verify | ❌ Solo SHA-256 | Baja |
| **`--row-hash-mode` flag** | §3 verify | ❌ | Baja |
| **`--tolerate-collation-diffs`** | §3 verify | ❌ | Baja |
| **`--ignore-definer-diffs` flag** | §3 verify | ⚠️ Auto-normalizado | Baja |
| **`--ignore-auto-inc` flag** | §3 verify | ⚠️ Auto-normalizado | Baja |
| **`--ignore-table-options` flag** | §3 verify | ❌ | Baja |
| **Config file (YAML/JSON) precedence docs** | §7 | ⚠️ Funcional, poca doc | Baja |
| **FK constraint verification** en schema verify | §5 | ❌ | Media |
| **Index verification** detallada | §5 | ⚠️ Via SHOW CREATE normalization | Media |
| **Partitioning verification** | §5 | ❌ | Media |

#### Prioridad BAJA (polish)

| Funcionalidad | Estado |
| --- | --- |
| **SBOM generation** | ❌ |
| **Reproducible builds documentation** | ❌ |
| **Versioning/release process** | ⚠️ Version hardcoded |
| **Performance/regression test suite** | ❌ |
| **Memory-bounded verification** con métricas | ⚠️ Parcial |
| **Progress bars / throughput metrics** | ❌ |
| **Rate limiting** en migración | ❌ |

### 5.2 Testing Gaps

| Área | Estado |
| --- | --- |
| Unit tests | ✅ Completos para todos los paquetes |
| Integration tests (Docker) | ✅ Scripts de test para la matriz v1 |
| End-to-end (crash simulation) | ❌ No automatizado |
| Performance/regression | ❌ No implementado |
| Negative tests automatizados | ⚠️ Parcial (algunos en unit tests) |
| CI integration test | ⚠️ Solo mysql84→mysql84 smoke |

### 5.3 Docker Compose Gap

`Instructions.md` requiere MariaDB 10.9 específicamente. El docker-compose usa MariaDB 10.6 como `mariadb10`. Esto puede ser intencional (10.6 es LTS) pero difiere de la especificación.

---

## 6. Análisis de Arquitectura

### 6.1 Fortalezas

1. **Arquitectura limpia y modular**: Separación clara entre CLI → commands → internal packages
2. **Dependency injection**: Funciones como `streamWindowEventsFn`, `loadTableMetadataFn` permiten testing sin DB real
3. **Manejo de errores excelente**: Exit codes estructurados, `applyFailure` con tipo/remediation/contexto
4. **Prechecks exhaustivos**: 15+ validadores que cubren los problemas reales de migración
5. **Checkpoint/resume**: Implementado para datos baseline y replicación
6. **Seguridad**: TLS required por defecto, DSN validation, file permissions privadas, redacción de samples
7. **Operator UX**: Propuestas de remediación en cada finding, artefactos persistentes para auditoría
8. **State-dir locking**: Single-writer lock con atomic writes

### 6.2 Debilidades

1. **Sin logging estructurado**: Todo va via `fmt.Fprintf`, no hay logger JSON configurable
2. **Sin concurrencia real**: El flag `--concurrency` existe pero la copia de datos es secuencial
3. **Schema verify superficial**: Compara `SHOW CREATE` normalizado, no metadata granular
4. **Sin routines/triggers/events**: Limita significativamente el scope de migraciones reales
5. **Observabilidad limitada**: No hay progress bars, throughput metrics, ni estimación de tiempo restante
6. **Dependencia directa de `go-mysql-org/go-mysql`**: Bien encapsulada detrás de interfaz, pero el paquete trae dependencias transitivas significativas

### 6.3 Dependencias (mínimas como se requirió)

```text
go-mysql-org/go-mysql v1.8.0     → binlog streaming
go-sql-driver/mysql v1.8.1       → database/sql driver
gopkg.in/yaml.v3                  → config file parsing
```

Solo 3 dependencias directas — excelente adherencia a la política de mínimas dependencias.

---

## 7. Exit Codes

| Código | Significado | Implementado |
| --- | --- | --- |
| 0 | Success | ✅ |
| 1 | Usage error | ✅ |
| 2 | Diffs/incompatibilities detected | ✅ |
| 3 | Migration/replication failed | ✅ |
| 4 | Verification failed (tool error) | ✅ |

---

## 8. Recomendaciones para Proseguir el Desarrollo

### Fase 1: Cerrar v1 Release (1-2 semanas)

**Objetivo:** Release estable de v1 con el scope actual.

1. **Ejecutar fresh full matrix run** contra Docker Compose con todas las combinaciones v1
2. **Ejecutar todos los rehearsals** y archivar resultados
3. **Completar signoff checklist** de `docs/v1-release-criteria.md`
4. **Tag v1.0.0** si pasa todos los gates
5. **Verificar CI smoke test** cubre al menos un escenario por engine-pair family

### Fase 2: Routines/Triggers/Events (2-3 semanas)

**Objetivo:** Completar migración de objetos de schema más allá de tables/views.

1. Extender `internal/schema/copy.go` para extraer y aplicar:
   - Stored procedures y functions (`SHOW CREATE PROCEDURE/FUNCTION`)
   - Triggers (`SHOW CREATE TRIGGER`)
   - Events (`SHOW CREATE EVENT`)
2. Extender `internal/verify/schema/verify.go` para comparar estos objetos
3. Actualizar `--include-objects` para aceptar `routines,triggers,events`
4. Tests unitarios e integración para cada tipo de objeto
5. Documentar las limitaciones cross-engine (DEFINER, delimiter handling)

### Fase 3: Structured Logging + Concurrencia (2 semanas)

1. **Implementar logger JSON** con levels (debug/info/warn/error)
2. **Concurrencia real** en `data.CopyBaselineData` usando goroutines + semáforo
3. **Progress reporting**: throughput, rows/sec, ETA por tabla
4. **Rate limiting** configurable

### Fase 4: Verificación Granular de Schema (1-2 semanas)

1. Verificación columna-por-columna via `INFORMATION_SCHEMA.COLUMNS`
2. Verificación de índices via `INFORMATION_SCHEMA.STATISTICS`
3. Verificación de FK constraints
4. Implementar flags `--tolerate-collation-diffs`, `--ignore-table-options`
5. Verificación de partitioning

### Fase 5: Trigger-CDC + Hybrid Mode (v2) (3-4 semanas)

1. Diseñar schema de CDC log tables
2. Implementar generación de triggers INSERT/UPDATE/DELETE por tabla
3. Implementar reader de CDC logs con checkpointing
4. Implementar `--teardown-cdc`
5. Implementar modo hybrid (binlog general + trigger-CDC selectivo)

### Fase 6: GTID Support (v2) (1-2 semanas)

1. Implementar `--start-from=gtid` para MySQL y MariaDB
2. Manejar la incompatibilidad MySQL GTID ↔ MariaDB GTID
3. Tests de integración

### Fase 7: User/Grant Migration (v2) (1-2 semanas)

1. Implementar extracción de `mysql.user` + grants
2. Filtro: solo business accounts vs. incluyendo system accounts
3. Reporte de incompatibilidades de auth plugins
4. Aplicación en destino con rollback safety

### Fase 8: HTML Report + Polish (v2) (1 semana)

1. Template HTML para reporte
2. `--idempotent` mode
3. SBOM, reproducible builds
4. Performance test suite

---

## 9. Resumen Cuantitativo

| Métrica | Valor |
| --- | --- |
| **Líneas de código Go (estimado)** | ~12,000+ |
| **Líneas de tests Go (estimado)** | ~8,000+ |
| **Paquetes con tests** | 12/13 (solo `version` sin test) |
| **PRs mergeados** | 85 |
| **Subcomandos implementados** | 5/5 |
| **Prechecks implementados** | 15+ |
| **Modos de verificación de datos** | 4/4 |
| **Modos de replicación** | 1/3 (binlog only) |
| **Tipos de objetos migrables** | 2/6 (tables, views) |
| **Scripts de test Docker** | 46 |
| **Configuraciones de migración** | 30 |
| **Documentación** | 13 docs, ~150KB total |
| **Dependencias directas** | 3 (excelente) |

---

## 10. Conclusión

El proyecto está en un estado **sólido para v1** con scope limitado a tables/views y binlog replication. La calidad del código es alta, los prechecks de compatibilidad son excepcionales, y la infraestructura de testing/CI está bien armada.

**Para llegar a producto final completo** según `Instructions.md`, se necesita:

- ~10-14 semanas adicionales de desarrollo
- Priorizar routines/triggers/events (impacto alto en utilidad real)
- Trigger-CDC es la funcionalidad más compleja pendiente

**Recomendación inmediata:** Cerrar y tagear v1.0.0 con el scope actual, luego iterar hacia v2 con las funcionalidades pendientes en el orden descrito.
