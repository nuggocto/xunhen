| Workload | Operation | n | Median | p95 | Slowest | Peak memory | Allocated (median) |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| small | inspect, cold file cache | 20 | 844 µs | 1.1 ms | 1.2 ms |  |  |
| small | inspect | 20 | 785 µs | 1.1 ms | 1.2 ms | at most 14 MiB |  |
| small | show reference | 20 | 804 µs | 896 µs | 1.0 ms | at most 14 MiB |  |
| small | show root, cold file cache | 20 | 828 µs | 957 µs | 1.1 ms |  |  |
| small | show root | 20 | 727 µs | 829 µs | 855 µs | at most 14 MiB |  |
| small | diff | 20 | 751 µs | 831 µs | 912 µs | at most 14 MiB |  |
| small | browser first state, cold file cache | 20 | 20.1 ms | 20.4 ms | 20.9 ms |  |  |
| small | browser first state | 20 | 19.8 ms | 20.1 ms | 20.2 ms |  |  |
| small | browser peak over comparison and two reloads | 20 |  |  |  | 17 MiB |  |
| small | browser quit, comparison already finished | 20 | 1.2 ms | 1.5 ms | 1.6 ms |  |  |
| small | load | 20 | 105 µs | 129 µs | 183 µs |  | 202 KiB in 726 allocations |
| small | preview, uncached | 20 | 14 µs | 22 µs | 23 µs |  | 9 KiB in 31 allocations |
| small | preview, cached | 20 | 1 µs | 1 µs | 1 µs |  |  |
| small | comparison, uncached | 20 | 54 µs | 76 µs | 76 µs |  | 44 KiB in 124 allocations |
| small | comparison, both states cached | 20 | 28 µs | 44 µs | 62 µs |  | 27 KiB in 84 allocations |
| small | comparison, cancelled halfway | 10 | 2 µs | n/a | 3 µs |  |  |
| small | heap after a comparison | 1 |  |  |  | 807 KiB live |  |
| small | heap while reloading | 1 |  |  |  | 852 KiB live |  |
| ordinary | inspect, cold file cache | 20 | 2.0 ms | 2.2 ms | 2.3 ms |  |  |
| ordinary | inspect | 20 | 1.8 ms | 2.0 ms | 2.3 ms | at most 14 MiB |  |
| ordinary | show reference | 20 | 1.7 ms | 1.9 ms | 2.0 ms | at most 14 MiB |  |
| ordinary | show root, cold file cache | 20 | 2.2 ms | 2.8 ms | 2.9 ms |  |  |
| ordinary | show root | 20 | 1.8 ms | 2.2 ms | 2.3 ms | at most 14 MiB |  |
| ordinary | diff | 20 | 1.9 ms | 2.4 ms | 2.5 ms | 14 MiB |  |
| ordinary | browser first state, cold file cache | 20 | 19.8 ms | 20.2 ms | 20.2 ms |  |  |
| ordinary | browser first state | 20 | 19.6 ms | 20.0 ms | 20.4 ms |  |  |
| ordinary | browser peak over comparison and two reloads | 20 |  |  |  | 22 MiB |  |
| ordinary | browser quit, comparison already finished | 20 | 1.3 ms | 1.6 ms | 1.8 ms |  |  |
| ordinary | load | 20 | 1.2 ms | 1.7 ms | 2.2 ms |  | 2.4 MiB in 19763 allocations |
| ordinary | preview, uncached | 20 | 25 µs | 50 µs | 115 µs |  | 154 KiB in 53 allocations |
| ordinary | preview, cached | 20 | 1 µs | 1 µs | 2 µs |  |  |
| ordinary | comparison, uncached | 20 | 169 µs | 237 µs | 320 µs |  | 475 KiB in 207 allocations |
| ordinary | comparison, both states cached | 20 | 121 µs | 145 µs | 147 µs |  | 224 KiB in 145 allocations |
| ordinary | comparison, cancelled halfway | 20 | 3 µs | 6 µs | 9 µs |  |  |
| ordinary | load, cancelled halfway | 20 | 34 µs | 96 µs | 100 µs |  |  |
| ordinary | heap after a comparison | 1 |  |  |  | 1.6 MiB live |  |
| ordinary | heap while reloading | 1 |  |  |  | 2.3 MiB live |  |
| deep | inspect, cold file cache | 20 | 151.6 ms | 155.5 ms | 156.9 ms |  |  |
| deep | inspect | 20 | 144.0 ms | 147.6 ms | 149.5 ms | 100 MiB |  |
| deep | show reference | 20 | 106.1 ms | 115.8 ms | 117.5 ms | 97 MiB |  |
| deep | show root, cold file cache | 20 | 124.3 ms | 135.5 ms | 137.0 ms |  |  |
| deep | show root | 20 | 116.2 ms | 122.7 ms | 127.3 ms | 100 MiB |  |
| deep | diff | 20 | 107.6 ms | 110.9 ms | 111.6 ms | 98 MiB |  |
| deep | browser first state, cold file cache | 20 | 120.6 ms | 137.0 ms | 137.1 ms |  |  |
| deep | browser first state | 20 | 120.1 ms | 121.1 ms | 121.2 ms |  |  |
| deep | browser peak over comparison and two reloads | 20 |  |  |  | 232 MiB |  |
| deep | browser quit, comparison already finished | 20 | 2.0 ms | 2.5 ms | 2.6 ms |  |  |
| deep | load | 20 | 98.4 ms | 103.1 ms | 106.0 ms |  | 257.7 MiB in 2098290 allocations |
| deep | preview, uncached | 20 | 7.8 ms | 10.9 ms | 11.3 ms |  | 195 KiB in 16 allocations |
| deep | preview, cached | 20 | 2 µs | 4 µs | 4 µs |  |  |
| deep | comparison, uncached | 20 | 8.8 ms | 12.2 ms | 13.3 ms |  | 380 KiB in 62 allocations |
| deep | comparison, both states cached | 20 | 32 µs | 49 µs | 54 µs |  | 10 KiB in 37 allocations |
| deep | comparison, cancelled halfway | 19 | 7 µs | n/a | 33 µs |  |  |
| deep | load, cancelled halfway | 20 | 7 µs | 120 µs | 477 µs |  |  |
| deep | heap after a comparison | 1 |  |  |  | 54.2 MiB live |  |
| deep | heap while reloading | 1 |  |  |  | 107.5 MiB live |  |
| wide | inspect, cold file cache | 20 | 60.9 ms | 63.3 ms | 63.4 ms |  |  |
| wide | inspect | 20 | 59.8 ms | 62.7 ms | 62.8 ms | 45 MiB |  |
| wide | show reference | 20 | 41.3 ms | 43.6 ms | 44.1 ms | 43 MiB |  |
| wide | show root, cold file cache | 20 | 44.3 ms | 46.1 ms | 46.7 ms |  |  |
| wide | show root | 20 | 42.5 ms | 45.0 ms | 45.3 ms | 43 MiB |  |
| wide | diff | 20 | 42.1 ms | 44.2 ms | 45.9 ms | 45 MiB |  |
| wide | browser first state, cold file cache | 20 | 53.4 ms | 70.7 ms | 71.0 ms |  |  |
| wide | browser first state | 20 | 53.3 ms | 54.3 ms | 54.5 ms |  |  |
| wide | browser peak over comparison and two reloads | 20 |  |  |  | 110 MiB |  |
| wide | browser quit, comparison already finished | 20 | 1.7 ms | 2.4 ms | 2.8 ms |  |  |
| wide | load | 20 | 41.6 ms | 42.8 ms | 43.5 ms |  | 102.1 MiB in 841126 allocations |
| wide | preview, uncached | 20 | 14 µs | 45 µs | 56 µs |  | 66 KiB in 15 allocations |
| wide | preview, cached | 20 | 2 µs | 4 µs | 4 µs |  |  |
| wide | comparison, uncached | 20 | 43 µs | 73 µs | 99 µs |  | 134 KiB in 60 allocations |
| wide | comparison, both states cached | 20 | 20 µs | 49 µs | 59 µs |  | 4 KiB in 36 allocations |
| wide | load, cancelled halfway | 20 | 6 µs | 81 µs | 1.3 ms |  |  |
| wide | comparison, cancelled halfway | 6 | 4 µs | n/a | 10 µs |  |  |
| wide | heap after a comparison | 1 |  |  |  | 22.7 MiB live |  |
| wide | heap while reloading | 1 |  |  |  | 44.6 MiB live |  |
| shuffled | inspect, cold file cache | 5 | 264.1 ms | n/a | 274.5 ms |  |  |
| shuffled | inspect | 5 | 257.0 ms | n/a | 271.2 ms | 225 MiB |  |
| shuffled | show reference | 5 | 448.6 ms | n/a | 458.0 ms | 562 MiB |  |
| shuffled | show root, cold file cache | 5 | 472.0 ms | n/a | 501.3 ms |  |  |
| shuffled | show root | 5 | 461.9 ms | n/a | 481.4 ms | 668 MiB |  |
| shuffled | diff | 5 | 1.32 s | n/a | 1.35 s | 786 MiB |  |
| shuffled | browser first state, cold file cache | 5 | 436.3 ms | n/a | 452.7 ms |  |  |
| shuffled | browser first state | 5 | 387.0 ms | n/a | 452.8 ms |  |  |
| shuffled | browser peak over comparison and two reloads | 5 |  |  |  | 821 MiB |  |
| shuffled | browser quit while comparing | 5 | 7.7 ms | n/a | 17.8 ms |  |  |
| shuffled | load | 5 | 281.5 ms | n/a | 304.2 ms |  | 681.2 MiB in 8000115 allocations |
| shuffled | preview, uncached | 5 | 48.3 ms | n/a | 63.1 ms |  | 244.4 MiB in 14 allocations |
| shuffled | preview, cached | 5 | 4 µs | n/a | 4 µs |  |  |
| shuffled | comparison, uncached | 5 | 817.2 ms | n/a | 834.5 ms |  | 954.9 MiB in 230 allocations |
| shuffled | comparison, both states cached | 5 | 742.8 ms | n/a | 749.5 ms |  | 588.3 MiB in 205 allocations |
| shuffled | comparison, cancelled halfway | 5 | 24.5 ms | n/a | 28.5 ms |  |  |
| shuffled | load, cancelled halfway | 5 | 10 µs | n/a | 11.0 ms |  |  |
| shuffled | heap after a comparison | 1 |  |  |  | 359.8 MiB live |  |
| shuffled | heap while reloading | 1 |  |  |  | 596.1 MiB live |  |
| repeated | inspect, cold file cache | 5 | 73.6 ms | n/a | 75.2 ms |  |  |
| repeated | inspect | 5 | 72.2 ms | n/a | 79.5 ms | 81 MiB |  |
| repeated | show reference | 5 | 111.2 ms | n/a | 125.2 ms | 155 MiB |  |
| repeated | show root, cold file cache | 5 | 132.0 ms | n/a | 140.1 ms |  |  |
| repeated | show root | 5 | 125.9 ms | n/a | 131.7 ms | 177 MiB |  |
| repeated | diff | 5 | 283.4 ms | n/a | 291.2 ms | 199 MiB |  |
| repeated | browser first state, cold file cache | 5 | 119.8 ms | n/a | 120.1 ms |  |  |
| repeated | browser first state | 5 | 119.3 ms | n/a | 120.6 ms |  |  |
| repeated | browser peak over comparison and two reloads | 5 |  |  |  | 282 MiB |  |
| repeated | browser quit while comparing | 5 | 2.1 ms | n/a | 2.5 ms |  |  |
| repeated | load | 5 | 65.5 ms | n/a | 70.5 ms |  | 155.6 MiB in 2000109 allocations |
| repeated | preview, uncached | 5 | 12.0 ms | n/a | 16.8 ms |  | 61.1 MiB in 14 allocations |
| repeated | preview, cached | 5 | 3 µs | n/a | 4 µs |  |  |
| repeated | comparison, uncached | 5 | 158.5 ms | n/a | 165.6 ms |  | 142.1 MiB in 73 allocations |
| repeated | comparison, both states cached | 5 | 142.5 ms | n/a | 146.9 ms |  | 50.4 MiB in 51 allocations |
| repeated | comparison, cancelled halfway | 5 | 16 µs | n/a | 20 µs |  |  |
| repeated | load, cancelled halfway | 5 | 8 µs | n/a | 11 µs |  |  |
| repeated | heap after a comparison | 1 |  |  |  | 86.5 MiB live |  |
| repeated | heap while reloading | 1 |  |  |  | 141.6 MiB live |  |
| replaced | inspect, cold file cache | 5 | 74.7 ms | n/a | 78.3 ms |  |  |
| replaced | inspect | 5 | 69.5 ms | n/a | 75.2 ms | 69 MiB |  |
| replaced | show reference | 5 | 122.6 ms | n/a | 132.8 ms | 151 MiB |  |
| replaced | show root, cold file cache | 5 | 132.1 ms | n/a | 135.4 ms |  |  |
| replaced | show root | 5 | 127.3 ms | n/a | 130.6 ms | 173 MiB |  |
| replaced | diff | 5 | 217.5 ms | n/a | 244.8 ms | 345 MiB |  |
| replaced | browser first state, cold file cache | 5 | 120.0 ms | n/a | 137.5 ms |  |  |
| replaced | browser first state | 5 | 119.6 ms | n/a | 136.3 ms |  |  |
| replaced | browser peak over comparison and two reloads | 5 |  |  |  | 428 MiB |  |
| replaced | browser quit while comparing | 5 | 2.1 ms | n/a | 3.0 ms |  |  |
| replaced | load | 5 | 75.2 ms | n/a | 78.5 ms |  | 175.0 MiB in 2000109 allocations |
| replaced | preview, uncached | 5 | 14.2 ms | n/a | 19.6 ms |  | 61.1 MiB in 14 allocations |
| replaced | preview, cached | 5 | 3 µs | n/a | 3 µs |  |  |
| replaced | comparison, uncached | 5 | 89.6 ms | n/a | 104.4 ms |  | 516.3 MiB in 181 allocations |
| replaced | comparison, both states cached | 5 | 76.4 ms | n/a | 79.6 ms |  | 424.6 MiB in 154 allocations |
| replaced | comparison, cancelled halfway | 5 | 14 µs | n/a | 60 µs |  |  |
| replaced | load, cancelled halfway | 5 | 1.6 ms | n/a | 3.2 ms |  |  |
| replaced | heap after a comparison | 1 |  |  |  | 130.3 MiB live |  |
| replaced | heap while reloading | 1 |  |  |  | 190.7 MiB live |  |
| changes-limit | inspect, cold file cache | 5 | 561.3 ms | n/a | 572.8 ms |  |  |
| changes-limit | inspect | 5 | 541.7 ms | n/a | 565.9 ms | 338 MiB |  |
| changes-limit | show reference | 5 | 423.7 ms | n/a | 445.9 ms | 313 MiB |  |
| changes-limit | show root, cold file cache | 5 | 490.4 ms | n/a | 510.2 ms |  |  |
| changes-limit | show root | 5 | 474.4 ms | n/a | 485.6 ms | 356 MiB |  |
| changes-limit | diff | 5 | 463.3 ms | n/a | 488.3 ms | 353 MiB |  |
| changes-limit | browser first state, cold file cache | 5 | 453.7 ms | n/a | 487.2 ms |  |  |
| changes-limit | browser first state | 5 | 437.3 ms | n/a | 453.9 ms |  |  |
| changes-limit | browser peak over comparison and two reloads | 5 |  |  |  | 762 MiB |  |
| changes-limit | browser quit while comparing | 5 | 3.0 ms | n/a | 3.5 ms |  |  |
| changes-limit | load | 5 | 404.1 ms | n/a | 466.0 ms |  | 980.9 MiB in 7842207 allocations |
| changes-limit | preview, uncached | 5 | 101.1 ms | n/a | 111.3 ms |  | 34.3 MiB in 560047 allocations |
| changes-limit | preview, cached | 5 | 4 µs | n/a | 5 µs |  |  |
| changes-limit | comparison, uncached | 5 | 114.0 ms | n/a | 134.5 ms |  | 34.9 MiB in 560076 allocations |
| changes-limit | comparison, both states cached | 5 | 44 µs | n/a | 71 µs |  | 4 KiB in 20 allocations |
| changes-limit | comparison, cancelled halfway | 5 | 27 µs | n/a | 53 µs |  |  |
| changes-limit | load, cancelled halfway | 5 | 9 µs | n/a | 3.7 ms |  |  |
| changes-limit | heap after a comparison | 1 |  |  |  | 203.8 MiB live |  |
| changes-limit | heap while reloading | 1 |  |  |  | 406.7 MiB live |  |
| entries-limit | inspect, cold file cache | 5 | 82.6 ms | n/a | 88.5 ms |  |  |
| entries-limit | inspect | 5 | 82.6 ms | n/a | 87.7 ms | 161 MiB |  |
| entries-limit | show reference | 5 | 285.5 ms | n/a | 304.5 ms | 485 MiB |  |
| entries-limit | show root, cold file cache | 5 | 412.4 ms | n/a | 431.3 ms |  |  |
| entries-limit | show root | 5 | 423.1 ms | n/a | 439.7 ms | 531 MiB |  |
| entries-limit | diff | 5 | 414.4 ms | n/a | 421.4 ms | 531 MiB |  |
| entries-limit | browser first state, cold file cache | 5 | 270.5 ms | n/a | 286.4 ms |  |  |
| entries-limit | browser first state | 5 | 253.3 ms | n/a | 270.5 ms |  |  |
| entries-limit | browser peak over comparison and two reloads | 5 |  |  |  | 726 MiB |  |
| entries-limit | browser quit while comparing | 5 | 2.9 ms | n/a | 3.5 ms |  |  |
| entries-limit | load | 5 | 186.2 ms | n/a | 187.3 ms |  | 463.2 MiB in 5000104 allocations |
| entries-limit | preview, uncached | 5 | 135.0 ms | n/a | 144.5 ms |  | 358.1 MiB in 27745 allocations |
| entries-limit | preview, cached | 5 | 4 µs | n/a | 5 µs |  |  |
| entries-limit | comparison, uncached | 5 | 198.7 ms | n/a | 212.0 ms |  | 480.3 MiB in 27764 allocations |
| entries-limit | comparison, both states cached | 5 | 5.4 ms | n/a | 5.6 ms |  | 784 B in 11 allocations |
| entries-limit | comparison, cancelled halfway | 5 | 6 µs | n/a | 15 µs |  |  |
| entries-limit | load, cancelled halfway | 5 | 11.2 ms | n/a | 12.4 ms |  |  |
| entries-limit | heap after a comparison | 1 |  |  |  | 259.6 MiB live |  |
| entries-limit | heap while reloading | 1 |  |  |  | 411.6 MiB live |  |
| lines-limit | inspect, cold file cache | 5 | 14.1 ms | n/a | 15.2 ms |  |  |
| lines-limit | inspect | 5 | 11.7 ms | n/a | 12.0 ms | 72 MiB |  |
| lines-limit | show reference | 5 | 61.0 ms | n/a | 61.2 ms | 202 MiB |  |
| lines-limit | show root, cold file cache | 5 | 65.0 ms | n/a | 65.4 ms |  |  |
| lines-limit | show root | 5 | 58.7 ms | n/a | 60.9 ms | 190 MiB |  |
| lines-limit | diff | 5 | 82.0 ms | n/a | 83.1 ms | 198 MiB |  |
| lines-limit | browser first state, cold file cache | 5 | 187.3 ms | n/a | 203.4 ms |  |  |
| lines-limit | browser first state | 5 | 169.9 ms | n/a | 186.2 ms |  |  |
| lines-limit | browser peak over comparison and two reloads | 5 |  |  |  | 366 MiB |  |
| lines-limit | browser quit while comparing | 5 | 3.0 ms | n/a | 3.2 ms |  |  |
| lines-limit | load | 5 | 44.1 ms | n/a | 46.8 ms |  | 243.2 MiB in 98 allocations |
| lines-limit | preview, uncached | 5 | 109.2 ms | n/a | 121.2 ms |  | 241 KiB in 27 allocations |
| lines-limit | preview, cached | 5 | 2 µs | n/a | 4 µs |  |  |
| lines-limit | comparison, uncached | 5 | 217.2 ms | n/a | 228.4 ms |  | 483 KiB in 71 allocations |
| lines-limit | comparison, both states cached | 5 | 2.8 ms | n/a | 2.9 ms |  | 1 KiB in 20 allocations |
| lines-limit | comparison, cancelled halfway | 5 | 221 µs | n/a | 337 µs |  |  |
| lines-limit | load, cancelled halfway | 5 | 18.4 ms | n/a | 21.4 ms |  |  |
| lines-limit | heap after a comparison | 1 |  |  |  | 68.4 MiB live |  |
| lines-limit | heap while reloading | 1 |  |  |  | 135.6 MiB live |  |
