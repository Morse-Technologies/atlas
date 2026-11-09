<div align="center">

# Atlas

### Asistente documental para cualquier organización

**Indexa. Pregunta. Cita. Audita.**  
Tus documentos se quedan donde están. Cada respuesta lleva su fuente.

[Inicio rápido](#inicio-rápido) | [Arquitectura](#arquitectura) | [Desarrollo](#desarrollo)

**Leer en:** [English](README.md) | [Español](README.es.md) | [中文](README.zh.md)

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev)
[![Next.js](https://img.shields.io/badge/Next.js-16-000000?style=for-the-badge&logo=nextdotjs&logoColor=white)](https://nextjs.org)
[![React](https://img.shields.io/badge/React-19-61DAFB?style=for-the-badge&logo=react&logoColor=black)](https://react.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?style=for-the-badge&logo=typescript&logoColor=white)](https://www.typescriptlang.org)
[![TailwindCSS](https://img.shields.io/badge/Tailwind-4-06B6D4?style=for-the-badge&logo=tailwindcss&logoColor=white)](https://tailwindcss.com)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-18-4169E1?style=for-the-badge&logo=postgresql&logoColor=white)](https://www.postgresql.org)

**Núcleo Apache-2.0** | Go | Next.js | Postgres | On-premise

</div>

---

> **Atlas** indexa la documentación de una organización allí donde está (carpetas de red, unidades compartidas, procedimientos, manuales, normas, correos) y responde preguntas en lenguaje natural con la cita exacta: documento, revisión, página y sección. Cuando falta evidencia, lo dice. Creado por [Morse Technologies](https://morsetechnologies.es) para equipos que necesitan respuestas fiables y trazables.

---

## ¿Qué es Atlas?

Atlas es un **asistente documental on-premise**. Lee tus fuentes sin mover ficheros, mantiene el índice al día por sí solo y deja que la gente pregunte como preguntaría a un compañero.

Cada respuesta enlaza al fragmento del que sale. Los permisos vienen de la fuente y se aplican antes de buscar, así que cada persona solo ve respuestas de documentos que ya puede abrir. Cada pregunta y respuesta queda registrada para auditoría, en línea con la trazabilidad del AI Act europeo.

Los modelos corren en local. Ningún documento sale de tu red.

```bash
# Frontend (datos simulados, sin backend)
cd app && pnpm install && pnpm dev
# App en http://localhost:3000

# Backend (Postgres, parsers, roles de atlas)
cd server && make up
# API en http://localhost:8080
```

---

## Qué puedes hacer

| Función | Core | Pro | Descripción |
|--------|------|-----|-------------|
| **Preguntar** | SÍ | SÍ | Preguntas en lenguaje natural, respuestas en streaming con citas numeradas. |
| **Documentos** | SÍ | SÍ | Explora el índice, abre el visor en la página citada, consulta metadata y revisiones. |
| **Fuentes** | SÍ | SÍ | Conecta carpetas locales y de red; Atlas las vigila y reindexa los cambios. |
| **Usuarios y ajustes** | SÍ | SÍ | Cuentas locales, roles, acceso con doble factor, borrado conforme al RGPD. |
| **Panel** | NO | SÍ | Resumen diario de actividad, cobertura e incidencias abiertas. |
| **Alertas de calidad** | NO | SÍ | Revisiones obsoletas, contradicciones, referencias rotas entre documentos. |
| **Mapa de conocimiento** | NO | SÍ | Grafo de documentos y de las entidades que mencionan. |
| **Informes de auditoría y SSO** | NO | SÍ | Registro de auditoría exportable, acceso con LDAP y Entra ID. |

*Las funciones Pro se desbloquean con una licencia firmada offline. La interfaz las bloquea y el backend lo hace cumplir.*

---

## Qué problema resuelve

- **Respuestas enterradas en ficheros.** Se abren diez PDF para encontrar un dato. Atlas encuentra el fragmento y lo muestra.
- **Respuestas que no se pueden comprobar.** Los chats genéricos suenan seguros sin fuente. Atlas nunca responde sin una.
- **Documentos que se desvían.** Revisiones antiguas, procedimientos que se contradicen, referencias perdidas. Atlas los saca a la luz antes que una auditoría.
- **Datos que no pueden salir.** Todo corre en tu servidor, aislado de internet si hace falta.

---

## Arquitectura

Monorepo por capas: backend, frontend, despliegue.

```
server/           Backend en Go: un binario (atlas) con roles api, worker, watch, migrate
  api/            openapi.yaml, el contrato compartido con el frontend
  db/             Migraciones y consultas SQL (sqlc)
  internal/       Módulos: access, catalog, documents, events, jobs, sources, httpapi
app/              Frontend Next.js: Preguntar, Documentos, Fuentes, Ajustes, Acceso
  src/lib/api/    Interfaz tipada AtlasApi y el adaptador mock hasta conectar la API
  src/ui/         Átomos, celdas, organismos, plantillas
deploy/compose/   Stack Docker Compose: Postgres 18 + pgvector, docling-serve, Gotenberg, roles de atlas
```

Postgres guarda datos, vectores, búsqueda de texto completo, la cola de trabajos (River) y la auditoría. El parseo (Docling), la conversión (Gotenberg), los embeddings y el LLM corren como servicios aparte, llamados por HTTP.

---

## Inicio rápido

### 1. Requisitos

- **Go 1.27+**
- **Node 20+** y **pnpm**
- **Docker** con Compose

```bash
git clone <repo-url>
cd atlas
```

### 2. Frontend

```bash
cd app
pnpm install
pnpm dev
```

App en **http://localhost:3000**. El frontend usa un mock tipado de la API con una organización ficticia de demo, así que funciona sin backend.

### 3. Backend

```bash
cd deploy/compose
cp .env.example .env          # pon las contraseñas
docker compose up -d --build
```

Después crea la organización y añade una fuente:

```bash
docker compose exec worker /atlas setup --org "Mi organización"
docker compose exec worker /atlas source add --name Docs --path /sources/docs
docker compose exec worker /atlas quarantine list
```

Pon los documentos en `deploy/compose/sources/` (o define `ATLAS_SOURCES_DIR`). API en **http://localhost:8080**.

---

## Impacto y público

- **Equipos de calidad, cumplimiento e IT** que necesitan respuestas trazables y un registro de auditoría.
- **Personas que hacen preguntas rápidas a diario** y necesitan la revisión correcta, con la página que lo demuestra.
- **Cualquier organización con documentación dispersa**: industria, despachos, sanidad, administración, servicios, educación.

---

## Desarrollo

```bash
# Backend
cd server
make gen                # regenera las consultas sqlc y el servidor OpenAPI
make lint               # golangci-lint
make test               # tests unitarios y de arquitectura
make test-integration   # Postgres real con testcontainers (Docker)

# Frontend
cd app
pnpm lint
pnpm build
sh scripts/check-punct.sh   # reglas de puntuación de la casa
```

Reglas de código: funciones de 20 líneas o menos, 3 parámetros o menos, nombres claros, sin números mágicos.

---

## Licencia

El núcleo es Apache-2.0. El nombre y el logo de Atlas son marcas de Morse Technologies y la licencia del código no los cubre. Atlas Pro es comercial.

---

<div align="center">

**Atlas** por Morse Technologies | Go | Next.js | Postgres | On-premise

</div>
