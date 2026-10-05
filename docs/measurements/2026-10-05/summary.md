| Workload | Operation | n | Median | p95 | Slowest | Peak memory | Allocated (median) |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| small | inspect, cold file cache | 20 | 893 µs | 1.2 ms | 1.3 ms |  |  |
| small | inspect | 20 | 834 µs | 1.1 ms | 1.1 ms | at most 14 MiB |  |
| small | show reference | 20 | 890 µs | 1.1 ms | 1.2 ms | at most 14 MiB |  |
| small | show root, cold file cache | 20 | 947 µs | 1.2 ms | 1.3 ms |  |  |
| small | show root | 20 | 841 µs | 1.2 ms | 1.2 ms | at most 14 MiB |  |
| small | diff | 20 | 838 µs | 935 µs | 1.1 ms | at most 14 MiB |  |
| small | browser first state, cold file cache | 20 | 20.2 ms | 20.5 ms | 20.5 ms |  |  |
| small | browser first state | 20 | 20.0 ms | 20.4 ms | 20.7 ms |  |  |
| small | browser peak over comparison and two reloads | 20 |  |  |  | 17 MiB |  |
| small | browser quit, comparison already finished | 20 | 1.2 ms | 1.6 ms | 1.8 ms |  |  |
| small | load | 20 | 97 µs | 131 µs | 188 µs |  | 197 KiB in 726 allocations |
| small | preview, uncached | 20 | 10 µs | 19 µs | 23 µs |  | 9 KiB in 31 allocations |
| small | preview, cached | 20 | 1 µs | 1 µs | 1 µs |  |  |
| small | comparison, uncached | 20 | 34 µs | 53 µs | 64 µs |  | 44 KiB in 124 allocations |
| small | comparison, both states cached | 20 | 29 µs | 49 µs | 51 µs |  | 27 KiB in 84 allocations |
| small | comparison, cancelled halfway | 17 | 2 µs | n/a | 5 µs |  |  |
| small | heap after a comparison | 1 |  |  |  | 801 KiB live |  |
| small | heap while reloading | 1 |  |  |  | 844 KiB live |  |
| ordinary | inspect, cold file cache | 20 | 2.1 ms | 2.4 ms | 2.4 ms |  |  |
| ordinary | inspect | 20 | 2.0 ms | 2.5 ms | 3.2 ms | at most 12 MiB |  |
| ordinary | show reference | 20 | 1.9 ms | 2.2 ms | 2.2 ms | 12 MiB |  |
| ordinary | show root, cold file cache | 20 | 2.3 ms | 2.9 ms | 3.0 ms |  |  |
| ordinary | show root | 20 | 1.9 ms | 2.3 ms | 2.5 ms | 12 MiB |  |
| ordinary | diff | 20 | 2.0 ms | 2.2 ms | 2.2 ms | at most 14 MiB |  |
| ordinary | browser first state, cold file cache | 20 | 20.0 ms | 20.5 ms | 20.8 ms |  |  |
| ordinary | browser first state | 20 | 19.8 ms | 20.2 ms | 20.4 ms |  |  |
| ordinary | browser peak over comparison and two reloads | 20 |  |  |  | 22 MiB |  |
| ordinary | browser quit, comparison already finished | 20 | 1.1 ms | 1.5 ms | 1.5 ms |  |  |
| ordinary | load | 20 | 1.1 ms | 1.5 ms | 1.6 ms |  | 2.2 MiB in 19762 allocations |
| ordinary | preview, uncached | 20 | 32 µs | 57 µs | 68 µs |  | 154 KiB in 53 allocations |
| ordinary | preview, cached | 20 | 1 µs | 1 µs | 2 µs |  |  |
| ordinary | comparison, uncached | 20 | 224 µs | 365 µs | 373 µs |  | 475 KiB in 207 allocations |
| ordinary | comparison, both states cached | 20 | 144 µs | 167 µs | 168 µs |  | 224 KiB in 145 allocations |
| ordinary | comparison, cancelled halfway | 20 | 3 µs | 6 µs | 6 µs |  |  |
| ordinary | load, cancelled halfway | 20 | 3 µs | 28 µs | 51 µs |  |  |
| ordinary | heap after a comparison | 1 |  |  |  | 1.5 MiB live |  |
| ordinary | heap while reloading | 1 |  |  |  | 2.2 MiB live |  |
| deep | inspect, cold file cache | 20 | 156.3 ms | 166.3 ms | 170.5 ms |  |  |
| deep | inspect | 20 | 154.1 ms | 164.5 ms | 167.2 ms | 90 MiB |  |
| deep | show reference | 20 | 112.0 ms | 119.3 ms | 119.5 ms | 89 MiB |  |
| deep | show root, cold file cache | 20 | 131.5 ms | 138.4 ms | 141.7 ms |  |  |
| deep | show root | 20 | 124.0 ms | 132.0 ms | 132.3 ms | 89 MiB |  |
| deep | diff | 20 | 109.0 ms | 115.9 ms | 119.5 ms | 89 MiB |  |
| deep | browser first state, cold file cache | 20 | 136.5 ms | 137.4 ms | 137.6 ms |  |  |
| deep | browser first state | 20 | 120.3 ms | 136.6 ms | 137.0 ms |  |  |
| deep | browser peak over comparison and two reloads | 20 |  |  |  | 187 MiB |  |
| deep | browser quit, comparison already finished | 20 | 1.8 ms | 2.9 ms | 3.2 ms |  |  |
| deep | load | 20 | 96.8 ms | 109.2 ms | 115.2 ms |  | 240.8 MiB in 2098290 allocations |
| deep | preview, uncached | 20 | 7.8 ms | 10.5 ms | 11.7 ms |  | 195 KiB in 16 allocations |
| deep | preview, cached | 20 | 3 µs | 4 µs | 4 µs |  |  |
| deep | comparison, uncached | 20 | 10.1 ms | 13.7 ms | 15.2 ms |  | 380 KiB in 62 allocations |
| deep | comparison, both states cached | 20 | 29 µs | 34 µs | 43 µs |  | 10 KiB in 37 allocations |
| deep | load, cancelled halfway | 20 | 7 µs | 8 µs | 358 µs |  |  |
| deep | comparison, cancelled halfway | 13 | 9 µs | n/a | 25 µs |  |  |
| deep | heap after a comparison | 1 |  |  |  | 50.7 MiB live |  |
| deep | heap while reloading | 1 |  |  |  | 100.5 MiB live |  |
| wide | inspect, cold file cache | 20 | 61.6 ms | 66.2 ms | 66.4 ms |  |  |
| wide | inspect | 20 | 59.5 ms | 62.4 ms | 62.7 ms | 47 MiB |  |
| wide | show reference | 20 | 41.6 ms | 43.1 ms | 44.6 ms | 47 MiB |  |
| wide | show root, cold file cache | 20 | 43.7 ms | 46.7 ms | 48.5 ms |  |  |
| wide | show root | 20 | 41.3 ms | 44.7 ms | 44.9 ms | 45 MiB |  |
| wide | diff | 20 | 41.9 ms | 44.4 ms | 45.0 ms | 45 MiB |  |
| wide | browser first state, cold file cache | 20 | 53.7 ms | 71.0 ms | 71.1 ms |  |  |
| wide | browser first state | 20 | 53.3 ms | 53.8 ms | 54.0 ms |  |  |
| wide | browser peak over comparison and two reloads | 20 |  |  |  | 105 MiB |  |
| wide | browser quit, comparison already finished | 20 | 1.7 ms | 2.3 ms | 2.3 ms |  |  |
| wide | load | 20 | 40.1 ms | 44.0 ms | 44.3 ms |  | 95.4 MiB in 841126 allocations |
| wide | preview, uncached | 20 | 13 µs | 19 µs | 19 µs |  | 66 KiB in 15 allocations |
| wide | preview, cached | 20 | 1 µs | 2 µs | 4 µs |  |  |
| wide | comparison, uncached | 20 | 30 µs | 48 µs | 90 µs |  | 134 KiB in 60 allocations |
| wide | comparison, both states cached | 20 | 17 µs | 22 µs | 23 µs |  | 4 KiB in 36 allocations |
| wide | comparison, cancelled halfway | 5 | 3 µs | n/a | 5 µs |  |  |
| wide | load, cancelled halfway | 20 | 7 µs | 8 µs | 9 µs |  |  |
| wide | heap after a comparison | 1 |  |  |  | 21.3 MiB live |  |
| wide | heap while reloading | 1 |  |  |  | 41.7 MiB live |  |
| shuffled | inspect, cold file cache | 5 | 266.5 ms | n/a | 313.6 ms |  |  |
| shuffled | inspect | 5 | 254.8 ms | n/a | 269.7 ms | 267 MiB |  |
| shuffled | show reference | 5 | 458.8 ms | n/a | 508.9 ms | 547 MiB |  |
| shuffled | show root, cold file cache | 5 | 492.9 ms | n/a | 519.1 ms |  |  |
| shuffled | show root | 5 | 469.0 ms | n/a | 526.7 ms | 696 MiB |  |
| shuffled | diff | 5 | 1.35 s | n/a | 1.41 s | 782 MiB |  |
| shuffled | browser first state, cold file cache | 5 | 486.1 ms | n/a | 519.4 ms |  |  |
| shuffled | browser first state | 5 | 453.5 ms | n/a | 485.7 ms |  |  |
| shuffled | browser peak over comparison and two reloads | 5 |  |  |  | 818 MiB |  |
| shuffled | browser quit while comparing | 5 | 7.9 ms | n/a | 9.5 ms |  |  |
| shuffled | load | 5 | 303.3 ms | n/a | 310.8 ms |  | 681.2 MiB in 8000115 allocations |
| shuffled | preview, uncached | 5 | 48.6 ms | n/a | 61.5 ms |  | 244.4 MiB in 14 allocations |
| shuffled | preview, cached | 5 | 4 µs | n/a | 6 µs |  |  |
| shuffled | comparison, uncached | 5 | 835.4 ms | n/a | 848.8 ms |  | 954.9 MiB in 229 allocations |
| shuffled | comparison, both states cached | 5 | 768.2 ms | n/a | 769.4 ms |  | 588.3 MiB in 208 allocations |
| shuffled | comparison, cancelled halfway | 5 | 196 µs | n/a | 21.2 ms |  |  |
| shuffled | load, cancelled halfway | 5 | 8 µs | n/a | 1.0 ms |  |  |
| shuffled | heap after a comparison | 1 |  |  |  | 359.8 MiB live |  |
| shuffled | heap while reloading | 1 |  |  |  | 596.1 MiB live |  |
| repeated | inspect, cold file cache | 5 | 78.0 ms | n/a | 79.9 ms |  |  |
| repeated | inspect | 5 | 68.5 ms | n/a | 71.9 ms | 77 MiB |  |
| repeated | show reference | 5 | 119.2 ms | n/a | 122.2 ms | 160 MiB |  |
| repeated | show root, cold file cache | 5 | 129.6 ms | n/a | 132.3 ms |  |  |
| repeated | show root | 5 | 123.4 ms | n/a | 132.0 ms | 173 MiB |  |
| repeated | diff | 5 | 289.1 ms | n/a | 294.0 ms | 198 MiB |  |
| repeated | browser first state, cold file cache | 5 | 119.7 ms | n/a | 135.9 ms |  |  |
| repeated | browser first state | 5 | 119.0 ms | n/a | 119.7 ms |  |  |
| repeated | browser peak over comparison and two reloads | 5 |  |  |  | 278 MiB |  |
| repeated | browser quit while comparing | 5 | 2.2 ms | n/a | 3.0 ms |  |  |
| repeated | load | 5 | 66.7 ms | n/a | 79.9 ms |  | 155.6 MiB in 2000109 allocations |
| repeated | preview, uncached | 5 | 11.8 ms | n/a | 17.8 ms |  | 61.1 MiB in 14 allocations |
| repeated | preview, cached | 5 | 3 µs | n/a | 4 µs |  |  |
| repeated | comparison, uncached | 5 | 165.7 ms | n/a | 171.2 ms |  | 142.1 MiB in 72 allocations |
| repeated | comparison, both states cached | 5 | 144.2 ms | n/a | 151.5 ms |  | 50.4 MiB in 47 allocations |
| repeated | comparison, cancelled halfway | 5 | 20 µs | n/a | 23 µs |  |  |
| repeated | load, cancelled halfway | 5 | 8 µs | n/a | 8 µs |  |  |
| repeated | heap after a comparison | 1 |  |  |  | 86.5 MiB live |  |
| repeated | heap while reloading | 1 |  |  |  | 141.6 MiB live |  |
| replaced | inspect, cold file cache | 5 | 65.8 ms | n/a | 77.5 ms |  |  |
| replaced | inspect | 5 | 69.2 ms | n/a | 75.3 ms | 67 MiB |  |
| replaced | show reference | 5 | 116.3 ms | n/a | 119.3 ms | 169 MiB |  |
| replaced | show root, cold file cache | 5 | 129.3 ms | n/a | 132.8 ms |  |  |
| replaced | show root | 5 | 125.5 ms | n/a | 126.3 ms | 179 MiB |  |
| replaced | diff | 5 | 221.0 ms | n/a | 221.3 ms | 348 MiB |  |
| replaced | browser first state, cold file cache | 5 | 135.7 ms | n/a | 135.9 ms |  |  |
| replaced | browser first state | 5 | 119.8 ms | n/a | 135.9 ms |  |  |
| replaced | browser peak over comparison and two reloads | 5 |  |  |  | 423 MiB |  |
| replaced | browser quit while comparing | 5 | 1.7 ms | n/a | 2.2 ms |  |  |
| replaced | load | 5 | 75.9 ms | n/a | 76.8 ms |  | 175.0 MiB in 2000109 allocations |
| replaced | preview, uncached | 5 | 12.3 ms | n/a | 12.8 ms |  | 61.1 MiB in 15 allocations |
| replaced | preview, cached | 5 | 3 µs | n/a | 4 µs |  |  |
| replaced | comparison, uncached | 5 | 98.8 ms | n/a | 103.0 ms |  | 516.3 MiB in 178 allocations |
| replaced | comparison, both states cached | 5 | 74.3 ms | n/a | 78.7 ms |  | 424.6 MiB in 157 allocations |
| replaced | comparison, cancelled halfway | 5 | 41 µs | n/a | 64 µs |  |  |
| replaced | load, cancelled halfway | 5 | 336 µs | n/a | 4.4 ms |  |  |
| replaced | heap after a comparison | 1 |  |  |  | 130.3 MiB live |  |
| replaced | heap while reloading | 1 |  |  |  | 190.7 MiB live |  |
| changes-limit | inspect, cold file cache | 5 | 616.7 ms | n/a | 654.3 ms |  |  |
| changes-limit | inspect | 5 | 618.0 ms | n/a | 626.0 ms | 308 MiB |  |
| changes-limit | show reference | 5 | 507.6 ms | n/a | 530.5 ms | 304 MiB |  |
| changes-limit | show root, cold file cache | 5 | 579.0 ms | n/a | 600.8 ms |  |  |
| changes-limit | show root | 5 | 538.8 ms | n/a | 594.0 ms | 331 MiB |  |
| changes-limit | diff | 5 | 544.7 ms | n/a | 653.7 ms | 332 MiB |  |
| changes-limit | browser first state, cold file cache | 5 | 519.5 ms | n/a | 619.7 ms |  |  |
| changes-limit | browser first state | 5 | 468.9 ms | n/a | 569.1 ms |  |  |
| changes-limit | browser peak over comparison and two reloads | 5 |  |  |  | 739 MiB |  |
| changes-limit | browser quit while comparing | 5 | 1.9 ms | n/a | 2.2 ms |  |  |
| changes-limit | load | 5 | 511.6 ms | n/a | 529.5 ms |  | 914.6 MiB in 7842208 allocations |
| changes-limit | preview, uncached | 5 | 137.3 ms | n/a | 170.8 ms |  | 34.3 MiB in 560047 allocations |
| changes-limit | preview, cached | 5 | 4 µs | n/a | 5 µs |  |  |
| changes-limit | comparison, uncached | 5 | 160.1 ms | n/a | 234.5 ms |  | 34.9 MiB in 560076 allocations |
| changes-limit | comparison, both states cached | 5 | 52 µs | n/a | 101 µs |  | 4 KiB in 20 allocations |
| changes-limit | comparison, cancelled halfway | 5 | 47 µs | n/a | 96 µs |  |  |
| changes-limit | load, cancelled halfway | 5 | 7 µs | n/a | 8 µs |  |  |
| changes-limit | heap after a comparison | 1 |  |  |  | 190.4 MiB live |  |
| changes-limit | heap while reloading | 1 |  |  |  | 380.0 MiB live |  |
| entries-limit | inspect, cold file cache | 5 | 85.7 ms | n/a | 93.3 ms |  |  |
| entries-limit | inspect | 5 | 87.6 ms | n/a | 91.1 ms | 160 MiB |  |
| entries-limit | show reference | 5 | 281.4 ms | n/a | 310.5 ms | 491 MiB |  |
| entries-limit | show root, cold file cache | 5 | 458.3 ms | n/a | 467.0 ms |  |  |
| entries-limit | show root | 5 | 438.8 ms | n/a | 459.1 ms | 530 MiB |  |
| entries-limit | diff | 5 | 414.4 ms | n/a | 417.9 ms | 531 MiB |  |
| entries-limit | browser first state, cold file cache | 5 | 270.1 ms | n/a | 303.5 ms |  |  |
| entries-limit | browser first state | 5 | 253.0 ms | n/a | 303.4 ms |  |  |
| entries-limit | browser peak over comparison and two reloads | 5 |  |  |  | 759 MiB |  |
| entries-limit | browser quit while comparing | 5 | 15.1 ms | n/a | 27.9 ms |  |  |
| entries-limit | load | 5 | 189.0 ms | n/a | 203.2 ms |  | 463.2 MiB in 5000104 allocations |
| entries-limit | preview, uncached | 5 | 135.6 ms | n/a | 136.5 ms |  | 358.1 MiB in 27745 allocations |
| entries-limit | preview, cached | 5 | 4 µs | n/a | 4 µs |  |  |
| entries-limit | comparison, uncached | 5 | 206.9 ms | n/a | 213.3 ms |  | 480.3 MiB in 27764 allocations |
| entries-limit | comparison, both states cached | 5 | 5.6 ms | n/a | 6.4 ms |  | 784 B in 11 allocations |
| entries-limit | comparison, cancelled halfway | 5 | 7 µs | n/a | 8 µs |  |  |
| entries-limit | load, cancelled halfway | 5 | 7 µs | n/a | 6.4 ms |  |  |
| entries-limit | heap after a comparison | 1 |  |  |  | 259.6 MiB live |  |
| entries-limit | heap while reloading | 1 |  |  |  | 411.6 MiB live |  |
| lines-limit | inspect, cold file cache | 5 | 16.5 ms | n/a | 18.1 ms |  |  |
| lines-limit | inspect | 5 | 13.5 ms | n/a | 15.1 ms | 64 MiB |  |
| lines-limit | show reference | 5 | 59.1 ms | n/a | 61.4 ms | 202 MiB |  |
| lines-limit | show root, cold file cache | 5 | 64.4 ms | n/a | 67.0 ms |  |  |
| lines-limit | show root | 5 | 58.8 ms | n/a | 62.0 ms | 191 MiB |  |
| lines-limit | diff | 5 | 90.4 ms | n/a | 90.7 ms | 196 MiB |  |
| lines-limit | browser first state, cold file cache | 5 | 186.0 ms | n/a | 186.1 ms |  |  |
| lines-limit | browser first state | 5 | 185.9 ms | n/a | 203.7 ms |  |  |
| lines-limit | browser peak over comparison and two reloads | 5 |  |  |  | 313 MiB |  |
| lines-limit | browser quit while comparing | 5 | 2.1 ms | n/a | 2.4 ms |  |  |
| lines-limit | load | 5 | 46.0 ms | n/a | 49.4 ms |  | 243.2 MiB in 98 allocations |
| lines-limit | preview, uncached | 5 | 116.7 ms | n/a | 124.6 ms |  | 241 KiB in 27 allocations |
| lines-limit | preview, cached | 5 | 3 µs | n/a | 5 µs |  |  |
| lines-limit | comparison, uncached | 5 | 228.0 ms | n/a | 239.3 ms |  | 483 KiB in 71 allocations |
| lines-limit | comparison, both states cached | 5 | 2.6 ms | n/a | 3.0 ms |  | 1 KiB in 20 allocations |
| lines-limit | comparison, cancelled halfway | 5 | 443 µs | n/a | 532 µs |  |  |
| lines-limit | load, cancelled halfway | 5 | 19.9 ms | n/a | 21.3 ms |  |  |
| lines-limit | heap after a comparison | 1 |  |  |  | 68.4 MiB live |  |
| lines-limit | heap while reloading | 1 |  |  |  | 135.6 MiB live |  |
| empty-lines | inspect, cold file cache | 5 | 121.6 ms | n/a | 123.9 ms |  |  |
| empty-lines | inspect | 5 | 114.2 ms | n/a | 116.9 ms | 229 MiB |  |
| empty-lines | show reference | 5 | 109.8 ms | n/a | 124.3 ms | 221 MiB |  |
| empty-lines | show root, cold file cache | 5 | 193.8 ms | n/a | 202.8 ms |  |  |
| empty-lines | show root | 5 | 197.7 ms | n/a | 213.3 ms | 424 MiB |  |
| empty-lines | diff | 5 | 209.9 ms | n/a | 218.9 ms | 321 MiB |  |
| empty-lines | browser first state, cold file cache | 5 | 136.7 ms | n/a | 137.6 ms |  |  |
| empty-lines | browser first state | 5 | 136.6 ms | n/a | 137.0 ms |  |  |
| empty-lines | browser peak over comparison and two reloads | 5 |  |  |  | 416 MiB |  |
| empty-lines | browser quit while comparing | 5 | 2.8 ms | n/a | 10.6 ms |  |  |
| empty-lines | load | 5 | 91.9 ms | n/a | 99.7 ms |  | 353.1 MiB in 4000115 allocations |
| empty-lines | preview, uncached | 5 | 24.0 ms | n/a | 37.0 ms |  | 183.4 MiB in 15 allocations |
| empty-lines | preview, cached | 5 | 4 µs | n/a | 5 µs |  |  |
| empty-lines | comparison, uncached | 5 | 51.9 ms | n/a | 52.2 ms |  | 198.6 MiB in 62 allocations |
| empty-lines | comparison, both states cached | 5 | 14.1 ms | n/a | 14.5 ms |  | 15.3 MiB in 39 allocations |
| empty-lines | comparison, cancelled halfway | 5 | 4.8 ms | n/a | 7.0 ms |  |  |
| empty-lines | load, cancelled halfway | 5 | 1.5 ms | n/a | 2.3 ms |  |  |
| empty-lines | heap after a comparison | 1 |  |  |  | 126.6 MiB live |  |
| empty-lines | heap while reloading | 1 |  |  |  | 191.2 MiB live |  |
