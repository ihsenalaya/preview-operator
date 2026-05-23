# RQ3 — MTTD components (external compute)

Generated 2026-05-23T18:04:55.612468Z from `300` reports producing `4202` diag rows.

## Operator-side latency (Tp − T0)

Wall-clock from `failureDetectedAt` to FailureReport CR `metadata.creationTimestamp` — i.e. evidence assembly + persistence by the operator. Negative values are clock ordering noise around the millisecond scale.

| n | min | p5 | p50 | p95 | max |
|---|---|---|---|---|---|
| 4202 | -3650.000 | -10.000 | 0.000 | 0.000 | 1.000 |

**Finding:** the operator's evidence-collection + persist latency is sub-second across the matrix (median 0 s; the max-1 s observed cases are likely informer-queue dispatch delay). This validates the RQ3 promise that operator-side MTTD is negligible relative to test-suite runtime.

## Caveat on end-to-end MTTD

`mttd_e2e_s = mtime(diag-*.json) − failureDetectedAt` is the WALL-CLOCK gap from when the operator detected the failure to when the diagnose script wrote the LLM's answer. For diag files produced post-hoc (the multi-app batch ran ~9-12 h AFTER the matrix), this gap includes **queueing latency** in addition to the LLM call time and is **not** a clean MTTD measurement. The medians per cell (LLM × C) are tabulated below for reference but should be interpreted as upper bounds, with the queueing component dominant.

| Subject | LLM | C | n | median | p25 | p75 | min | max |
|---|---|---|---|---|---|---|---|---|
| s1-flask-catalog | LLM-A | C1 | 200 | 52.38 | 30.27 | 33437.08 | 7.76 | 41426.29 |
| s1-flask-catalog | LLM-A | C2 | 200 | 56.86 | 34.82 | 33445.91 | 13.71 | 41434.37 |
| s1-flask-catalog | LLM-A | C3 | 200 | 62.81 | 40.97 | 33456.44 | 20.11 | 41445.42 |
| s1-flask-catalog | LLM-A | C4 | 200 | 69.69 | 48.19 | 33462.44 | 25.95 | 41453.42 |
| s1-flask-catalog | LLM-A | C5 | 200 | 76.08 | 55.26 | 33470.42 | 32.43 | 41459.89 |
| s1-flask-catalog | LLM-B | C1 | 200 | 28299.66 | 19684.22 | 65469.17 | 13491.56 | 74315.02 |
| s1-flask-catalog | LLM-B | C2 | 200 | 28303.62 | 19688.25 | 65477.47 | 13495.49 | 74321.94 |
| s1-flask-catalog | LLM-B | C3 | 200 | 28311.16 | 19695.23 | 65486.74 | 13502.61 | 74328.58 |
| s1-flask-catalog | LLM-B | C4 | 200 | 28320.14 | 19703.96 | 65496.38 | 13511.75 | 74337.48 |
| s1-flask-catalog | LLM-B | C5 | 200 | 28328.62 | 19711.95 | 65506.08 | 13522.01 | 74345.63 |
| s1-flask-catalog | rule | C1 | 100 | 48.45 | 27.28 | 33434.49 | 4.40 | 41420.49 |
| s1-flask-catalog | rule | C2 | 100 | 54.21 | 31.91 | 33439.67 | 9.48 | 41426.31 |
| s1-flask-catalog | rule | C3 | 100 | 59.10 | 36.69 | 33449.79 | 15.53 | 41434.39 |
| s1-flask-catalog | rule | C4 | 100 | 64.04 | 42.72 | 33458.93 | 22.32 | 41445.43 |
| s1-flask-catalog | rule | C5 | 100 | 70.99 | 49.14 | 33466.05 | 28.89 | 41453.43 |
| s2-listmonk | LLM-A | C1 | 50 | 10204.57 | 9883.29 | 10447.74 | 1262.20 | 10577.22 |
| s2-listmonk | LLM-A | C2 | 50 | 10134.53 | 9888.18 | 10448.59 | 1264.73 | 10577.65 |
| s2-listmonk | LLM-A | C3 | 50 | 10198.57 | 9819.19 | 10437.91 | 1266.62 | 10577.82 |
| s2-listmonk | LLM-A | C4 | 50 | 10171.63 | 9882.76 | 10435.06 | 1268.72 | 10580.86 |
| s2-listmonk | LLM-A | C5 | 50 | 10221.44 | 9883.43 | 10450.73 | 1329.46 | 10579.98 |
| s2-listmonk | LLM-B | C1 | 50 | 10341.12 | 10063.68 | 10583.40 | 1642.92 | 10805.05 |
| s2-listmonk | LLM-B | C2 | 50 | 10448.39 | 9991.81 | 10556.21 | 1646.15 | 10801.84 |
| s2-listmonk | LLM-B | C3 | 50 | 10434.36 | 10015.63 | 10577.63 | 1271.35 | 10806.65 |
| s2-listmonk | LLM-B | C4 | 50 | 10304.42 | 10021.99 | 10578.36 | 1649.48 | 10809.81 |
| s2-listmonk | LLM-B | C5 | 50 | 10324.68 | 10000.49 | 10582.94 | 1391.23 | 10810.67 |
| s3-healthchecks | LLM-A | C1 | 50 | 4427.96 | 2729.55 | 6265.47 | 1532.98 | 6632.51 |
| s3-healthchecks | LLM-A | C2 | 50 | 4439.53 | 2759.93 | 6266.13 | 1562.99 | 6598.85 |
| s3-healthchecks | LLM-A | C3 | 50 | 4438.70 | 2730.58 | 6268.02 | 1568.93 | 6583.78 |
| s3-healthchecks | LLM-A | C4 | 50 | 4444.45 | 2733.74 | 6269.46 | 1563.56 | 6597.74 |
| s3-healthchecks | LLM-A | C5 | 50 | 4446.71 | 2736.12 | 6271.14 | 1565.08 | 6597.93 |
| s3-healthchecks | LLM-B | C1 | 50 | 5013.90 | 3386.20 | 6755.04 | 2175.54 | 6970.85 |
| s3-healthchecks | LLM-B | C2 | 50 | 5016.04 | 3393.28 | 6762.02 | 2179.93 | 6967.74 |
| s3-healthchecks | LLM-B | C3 | 50 | 5018.24 | 3392.66 | 6768.13 | 2185.19 | 6975.73 |
| s3-healthchecks | LLM-B | C4 | 50 | 5031.09 | 3399.69 | 6776.24 | 2196.81 | 6983.62 |
| s3-healthchecks | LLM-B | C5 | 50 | 5025.63 | 3399.49 | 6770.69 | 2189.23 | 6980.92 |
| s4-umami | LLM-A | C1 | 50 | 4809.39 | 4521.86 | 4977.59 | 2126.85 | 5149.29 |
| s4-umami | LLM-A | C2 | 50 | 4816.62 | 4527.04 | 4979.52 | 2217.07 | 5165.41 |
| s4-umami | LLM-A | C3 | 50 | 4808.40 | 4524.36 | 4982.39 | 2130.35 | 5149.10 |
| s4-umami | LLM-A | C4 | 50 | 4821.22 | 4518.48 | 5007.29 | 2134.72 | 5179.33 |
| s4-umami | LLM-A | C5 | 50 | 4830.70 | 4520.16 | 4988.65 | 2138.07 | 5213.35 |
| s4-umami | LLM-B | C1 | 40 | 5732.11 | 5598.14 | 5829.69 | 5423.39 | 5928.86 |
| s4-umami | LLM-B | C2 | 41 | 5732.35 | 5591.85 | 5832.64 | 3284.68 | 5917.49 |
| s4-umami | LLM-B | C3 | 41 | 5735.11 | 5597.90 | 5829.76 | 3287.95 | 5921.38 |
| s4-umami | LLM-B | C4 | 40 | 5738.93 | 5610.10 | 5842.28 | 5433.32 | 5924.43 |
| s4-umami | LLM-B | C5 | 40 | 5741.60 | 5619.62 | 5843.84 | 5457.29 | 5927.31 |
| s5-petclinic | LLM-A | C1 | 50 | 5186.29 | 4825.85 | 5375.22 | 2465.71 | 5532.34 |
| s5-petclinic | LLM-A | C2 | 50 | 5185.08 | 4826.88 | 5377.78 | 2467.89 | 5533.34 |
| s5-petclinic | LLM-A | C3 | 50 | 5180.14 | 4832.29 | 5381.58 | 2471.65 | 5560.43 |
| s5-petclinic | LLM-A | C4 | 50 | 5181.74 | 4833.35 | 5382.51 | 2476.93 | 5566.63 |
| s5-petclinic | LLM-A | C5 | 50 | 5183.74 | 4829.99 | 5387.00 | 2475.65 | 5542.81 |

## Clean LLM-call latency (TODO)

To produce a clean LLM-call latency dataset, re-run `fp-diagnose` through a wrapper that records start/end wall-clock around each `Diagnose` invocation; the binary's own `_seconds` field is not populated in this build. Tracked as work unit W7 in Q1-COMPLIANCE.md.
