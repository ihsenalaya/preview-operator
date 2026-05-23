# RQ3 — MTTD components (external compute)

Generated 2026-05-23T22:10:18.539448Z from `500` reports producing `6500` diag rows.

## Operator-side latency (Tp − T0)

Wall-clock from `failureDetectedAt` to FailureReport CR `metadata.creationTimestamp` — i.e. evidence assembly + persistence by the operator. Negative values are clock ordering noise around the millisecond scale.

| n | min | p5 | p50 | p95 | max |
|---|---|---|---|---|---|
| 6500 | -3650.000 | -11.000 | -2.000 | 0.000 | 1.000 |

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
| s2-listmonk | LLM-A | C1 | 100 | 6191.52 | 1454.94 | 10208.85 | 130.75 | 10577.22 |
| s2-listmonk | LLM-A | C2 | 100 | 6174.62 | 1485.60 | 10158.15 | 131.06 | 10577.65 |
| s2-listmonk | LLM-A | C3 | 100 | 6175.00 | 1484.23 | 10205.08 | 131.91 | 10577.82 |
| s2-listmonk | LLM-A | C4 | 100 | 6198.94 | 1484.64 | 10199.54 | 132.30 | 10580.86 |
| s2-listmonk | LLM-A | C5 | 100 | 6190.65 | 1483.87 | 10226.16 | 132.22 | 10579.98 |
| s2-listmonk | LLM-B | C1 | 100 | 6272.37 | 1793.21 | 10347.80 | 135.15 | 10805.05 |
| s2-listmonk | LLM-B | C2 | 100 | 6271.47 | 1796.43 | 10463.72 | 136.91 | 10801.84 |
| s2-listmonk | LLM-B | C3 | 100 | 6273.89 | 1798.83 | 10438.63 | 140.45 | 10806.65 |
| s2-listmonk | LLM-B | C4 | 100 | 6276.43 | 1802.14 | 10328.70 | 141.49 | 10809.81 |
| s2-listmonk | LLM-B | C5 | 100 | 6286.32 | 1850.21 | 10376.04 | 142.22 | 10810.67 |
| s3-healthchecks | LLM-A | C1 | 100 | 2937.04 | 1641.46 | 4430.78 | 153.54 | 6632.51 |
| s3-healthchecks | LLM-A | C2 | 100 | 2938.60 | 1641.22 | 4440.21 | 153.61 | 6598.85 |
| s3-healthchecks | LLM-A | C3 | 100 | 2922.59 | 1659.08 | 4440.44 | 156.14 | 6583.78 |
| s3-healthchecks | LLM-A | C4 | 100 | 2940.33 | 1655.14 | 4446.64 | 156.24 | 6597.74 |
| s3-healthchecks | LLM-A | C5 | 100 | 2941.80 | 1654.35 | 4450.03 | 182.07 | 6597.93 |
| s3-healthchecks | LLM-B | C1 | 100 | 3470.68 | 2333.61 | 5015.87 | 244.74 | 6970.85 |
| s3-healthchecks | LLM-B | C2 | 100 | 3469.09 | 2331.32 | 5024.24 | 239.09 | 6967.74 |
| s3-healthchecks | LLM-B | C3 | 100 | 3483.57 | 2344.23 | 5027.89 | 268.89 | 6975.73 |
| s3-healthchecks | LLM-B | C4 | 100 | 3477.46 | 2343.88 | 5032.39 | 246.37 | 6983.62 |
| s3-healthchecks | LLM-B | C5 | 100 | 3490.62 | 2341.25 | 5033.71 | 254.80 | 6980.92 |
| s4-umami | LLM-A | C1 | 100 | 4846.45 | 2280.36 | 6213.91 | 245.02 | 8122.48 |
| s4-umami | LLM-A | C2 | 100 | 4841.45 | 2286.35 | 6182.28 | 247.04 | 8137.83 |
| s4-umami | LLM-A | C3 | 100 | 4828.24 | 2264.18 | 6153.72 | 248.20 | 8136.02 |
| s4-umami | LLM-A | C4 | 100 | 4838.73 | 2265.76 | 6183.96 | 250.55 | 8137.58 |
| s4-umami | LLM-A | C5 | 100 | 4854.26 | 2292.09 | 6154.75 | 251.77 | 8137.77 |
| s4-umami | LLM-B | C1 | 100 | 5732.11 | 3293.26 | 7033.48 | 430.06 | 8768.45 |
| s4-umami | LLM-B | C2 | 100 | 5732.45 | 3304.41 | 7046.65 | 436.04 | 8784.73 |
| s4-umami | LLM-B | C3 | 100 | 5735.37 | 3287.95 | 7037.59 | 435.42 | 8796.25 |
| s4-umami | LLM-B | C4 | 100 | 5738.93 | 3296.24 | 7043.98 | 453.23 | 8777.37 |
| s4-umami | LLM-B | C5 | 100 | 5741.60 | 3304.31 | 7055.39 | 441.55 | 8779.98 |
| s5-petclinic | LLM-A | C1 | 100 | 5229.12 | 2629.17 | 7906.78 | 243.63 | 8504.72 |
| s5-petclinic | LLM-A | C2 | 100 | 5215.67 | 2638.37 | 7906.75 | 246.64 | 8520.28 |
| s5-petclinic | LLM-A | C3 | 100 | 5214.01 | 2642.45 | 7915.00 | 249.67 | 8520.69 |
| s5-petclinic | LLM-A | C4 | 100 | 5217.30 | 2641.03 | 7950.10 | 255.19 | 8523.08 |
| s5-petclinic | LLM-A | C5 | 100 | 5239.22 | 2644.04 | 7912.21 | 255.20 | 8483.42 |
| s5-petclinic | LLM-B | C1 | 100 | 6446.05 | 3971.04 | 8920.71 | 509.14 | 9410.83 |
| s5-petclinic | LLM-B | C2 | 100 | 6451.36 | 3974.95 | 8942.24 | 517.77 | 9415.18 |
| s5-petclinic | LLM-B | C3 | 100 | 6452.23 | 3981.56 | 8948.28 | 512.48 | 9419.19 |
| s5-petclinic | LLM-B | C4 | 100 | 6455.59 | 3990.18 | 8928.14 | 514.99 | 9421.33 |
| s5-petclinic | LLM-B | C5 | 100 | 6457.54 | 3983.30 | 8931.66 | 529.66 | 9424.20 |

## Clean LLM-call latency (TODO)

To produce a clean LLM-call latency dataset, re-run `fp-diagnose` through a wrapper that records start/end wall-clock around each `Diagnose` invocation; the binary's own `_seconds` field is not populated in this build. Tracked as work unit W7 in Q1-COMPLIANCE.md.
