# Preview Operator — Simple HLD

```
┌─────────────────────────────────────────────────────────────────────┐
│                          GITHUB                                     │
│                    (PR opened/updated)                              │
└──────────────────────────┬──────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────────────┐
│                  PREVIEW OPERATOR (Controller)                      │
│                                                                     │
│  • Watches Preview CRs                                             │
│  • Orchestrates everything                                         │
│  • Creates/deletes resources                                       │
└──────────────────────────┬──────────────────────────────────────────┘
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
        ▼                  ▼                  ▼
    ┌────────┐         ┌────────┐         ┌────────┐
    │Preview │         │TestPlan│         │TestRun │
    │  (CRD) │         │ (CRD)  │         │ (CRD)  │
    └────────┘         └────────┘         └────────┘
        │                  ▲                  │
        │                  │                  │
        │         test-strategist-agent       │
        │         (reads changeContext)       │
        │         (writes mustRun/canSkip)    │
        │                                     │
        ▼                                     ▼
    ┌────────────┐                    ┌──────────────┐
    │ReconcileEv.│                    │FailureReport │
    │   (CRD)    │                    │   (CRD)      │
    └────────────┘                    └──────────────┘
                                            │
                                            │
                                   failure-analyst-agent
                                   (reads evidence)
                                   (writes diagnoses)
        │
        └─────────────────────────┐
                                  │
                                  ▼
                        ┌──────────────────┐
                        │  kagent Agents   │
                        │                  │
                        │ • test-strategist│
                        │ • troubleshooter │
                        │ • diff-analyzer  │
                        │                  │
                        │ MCP Servers:     │
                        │ • k8s-tools      │
                        │ • jaeger         │
                        │ • github         │
                        └────────┬─────────┘
                                 │
                                 ▼
                        ┌──────────────────┐
                        │    GITHUB        │
                        │   (PR comment)   │
                        │ (deployment sts) │
                        └──────────────────┘
```

## Composants clés

| Composant | Rôle |
|-----------|------|
| **Preview Operator** | Orchestrateur principal - contrôle tout |
| **Preview CRD** | Demande de preview environment |
| **TestPlan CRD** | Plan de test (quels tests run) |
| **TestRun CRD** | Résultats des tests |
| **ReconcileEvent CRD** | Log des événements |
| **FailureReport CRD** | Diagnostic des erreurs |
| **test-strategist-agent** | Choisit les tests à runner |
| **failure-analyst-agent** | Analyse les erreurs |
| **preview-diff-analyzer** | Analyse les changements du PR |

## Flow simple

```
1. PR opened
   ↓
2. Operator crée Preview CR
   ↓
3. Operator crée TestPlan
   ↓
4. test-strategist-agent décide: mustRun vs canSkip
   ↓
5. Operator run tests → TestRun
   ↓
6. Si erreur: FailureReport + failure-analyst-agent
   ↓
7. Operator poste PR comment avec résultats
```
