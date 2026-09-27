| Workload | Operation | n | Median | p95 | Slowest | Peak memory | Allocated (median) |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| small | inspect, cold file cache | 20 | 864 µs | 1.0 ms | 1.1 ms |  |  |
| small | inspect | 20 | 817 µs | 1.1 ms | 1.1 ms | at most 14 MiB |  |
| small | show reference | 20 | 796 µs | 953 µs | 975 µs | at most 14 MiB |  |
| small | show root, cold file cache | 20 | 835 µs | 911 µs | 1.2 ms |  |  |
| small | show root | 20 | 787 µs | 896 µs | 898 µs | at most 14 MiB |  |
| small | diff | 20 | 727 µs | 863 µs | 936 µs | at most 14 MiB |  |
| small | browser first state, cold file cache | 20 | 20.0 ms | 20.6 ms | 21.3 ms |  |  |
| small | browser first state | 20 | 19.9 ms | 20.2 ms | 20.5 ms |  |  |
| small | browser peak over comparison and two reloads | 20 |  |  |  | 17 MiB |  |
| small | browser quit, comparison already finished | 20 | 1.1 ms | 1.7 ms | 2.0 ms |  |  |
| small | load | 20 | 98 µs | 142 µs | 157 µs |  | 202 KiB in 726 allocations |
| small | preview, uncached | 20 | 13 µs | 22 µs | 28 µs |  | 9 KiB in 31 allocations |
| small | preview, cached | 20 | 1 µs | 1 µs | 2 µs |  |  |
| small | comparison, uncached | 20 | 51 µs | 66 µs | 79 µs |  | 60 KiB in 151 allocations |
| small | comparison, both states cached | 20 | 30 µs | 49 µs | 53 µs |  | 43 KiB in 111 allocations |
| small | comparison, cancelled halfway | 16 | 4 µs | n/a | 14 µs |  |  |
| small | heap after a comparison | 1 |  |  |  | 815 KiB live |  |
| small | heap while reloading | 1 |  |  |  | 860 KiB live |  |
| ordinary | inspect, cold file cache | 20 | 2.0 ms | 2.4 ms | 2.6 ms |  |  |
| ordinary | inspect | 20 | 1.8 ms | 2.1 ms | 2.2 ms | at most 14 MiB |  |
| ordinary | show reference | 20 | 1.8 ms | 2.0 ms | 2.1 ms | at most 14 MiB |  |
| ordinary | show root, cold file cache | 20 | 2.1 ms | 2.6 ms | 2.7 ms |  |  |
| ordinary | show root | 20 | 1.8 ms | 2.0 ms | 2.0 ms | at most 14 MiB |  |
| ordinary | diff | 20 | 2.4 ms | 2.7 ms | 2.7 ms | 14 MiB |  |
| ordinary | browser first state, cold file cache | 20 | 19.8 ms | 20.2 ms | 20.4 ms |  |  |
| ordinary | browser first state | 20 | 19.7 ms | 20.0 ms | 20.2 ms |  |  |
| ordinary | browser peak over comparison and two reloads | 20 |  |  |  | 20 MiB |  |
| ordinary | browser quit, comparison already finished | 20 | 1.1 ms | 1.5 ms | 1.5 ms |  |  |
| ordinary | load | 20 | 1.4 ms | 1.7 ms | 1.8 ms |  | 2.4 MiB in 19763 allocations |
| ordinary | preview, uncached | 20 | 35 µs | 46 µs | 54 µs |  | 154 KiB in 53 allocations |
| ordinary | preview, cached | 20 | 1 µs | 1 µs | 1 µs |  |  |
| ordinary | comparison, uncached | 20 | 200 µs | 254 µs | 312 µs |  | 716 KiB in 259 allocations |
| ordinary | comparison, both states cached | 20 | 153 µs | 174 µs | 202 µs |  | 464 KiB in 197 allocations |
| ordinary | comparison, cancelled halfway | 20 | 2 µs | 9 µs | 10 µs |  |  |
| ordinary | load, cancelled halfway | 20 | 14 µs | 71 µs | 76 µs |  |  |
| ordinary | heap after a comparison | 1 |  |  |  | 1.7 MiB live |  |
| ordinary | heap while reloading | 1 |  |  |  | 2.4 MiB live |  |
| deep | inspect, cold file cache | 20 | 152.8 ms | 157.1 ms | 160.4 ms |  |  |
| deep | inspect | 20 | 144.6 ms | 149.9 ms | 158.9 ms | 98 MiB |  |
| deep | show reference | 20 | 108.1 ms | 114.1 ms | 117.6 ms | 99 MiB |  |
| deep | show root, cold file cache | 20 | 127.1 ms | 131.7 ms | 131.9 ms |  |  |
| deep | show root | 20 | 119.2 ms | 123.6 ms | 125.9 ms | 101 MiB |  |
| deep | diff | 20 | 108.2 ms | 119.5 ms | 121.6 ms | 97 MiB |  |
| deep | browser first state, cold file cache | 20 | 136.2 ms | 137.5 ms | 137.6 ms |  |  |
| deep | browser first state | 20 | 120.6 ms | 137.1 ms | 137.1 ms |  |  |
| deep | browser peak over comparison and two reloads | 20 |  |  |  | 243 MiB |  |
| deep | browser quit, comparison already finished | 20 | 2.0 ms | 2.4 ms | 2.6 ms |  |  |
| deep | load | 20 | 97.9 ms | 106.5 ms | 108.0 ms |  | 257.7 MiB in 2098290 allocations |
| deep | preview, uncached | 20 | 7.7 ms | 10.6 ms | 10.8 ms |  | 195 KiB in 16 allocations |
| deep | preview, cached | 20 | 2 µs | 4 µs | 4 µs |  |  |
| deep | comparison, uncached | 20 | 7.9 ms | 10.9 ms | 11.1 ms |  | 408 KiB in 84 allocations |
| deep | comparison, both states cached | 20 | 29 µs | 55 µs | 57 µs |  | 38 KiB in 59 allocations |
| deep | comparison, cancelled halfway | 16 | 12 µs | n/a | 23 µs |  |  |
| deep | load, cancelled halfway | 20 | 8 µs | 10 µs | 10 µs |  |  |
| deep | heap after a comparison | 1 |  |  |  | 54.2 MiB live |  |
| deep | heap while reloading | 1 |  |  |  | 107.5 MiB live |  |
| wide | inspect, cold file cache | 20 | 62.4 ms | 65.4 ms | 66.4 ms |  |  |
| wide | inspect | 20 | 60.2 ms | 64.2 ms | 64.7 ms | 45 MiB |  |
| wide | show reference | 20 | 42.8 ms | 45.3 ms | 47.5 ms | 43 MiB |  |
| wide | show root, cold file cache | 20 | 45.1 ms | 47.8 ms | 48.5 ms |  |  |
| wide | show root | 20 | 41.9 ms | 44.9 ms | 45.9 ms | 45 MiB |  |
| wide | diff | 20 | 41.9 ms | 43.5 ms | 45.4 ms | 45 MiB |  |
| wide | browser first state, cold file cache | 20 | 53.5 ms | 70.8 ms | 71.4 ms |  |  |
| wide | browser first state | 20 | 53.4 ms | 53.9 ms | 54.0 ms |  |  |
| wide | browser peak over comparison and two reloads | 20 |  |  |  | 111 MiB |  |
| wide | browser quit, comparison already finished | 20 | 1.8 ms | 2.0 ms | 2.0 ms |  |  |
| wide | load | 20 | 40.6 ms | 43.6 ms | 44.0 ms |  | 102.1 MiB in 841126 allocations |
| wide | preview, uncached | 20 | 14 µs | 26 µs | 49 µs |  | 66 KiB in 15 allocations |
| wide | preview, cached | 20 | 2 µs | 4 µs | 4 µs |  |  |
| wide | comparison, uncached | 20 | 45 µs | 69 µs | 76 µs |  | 136 KiB in 72 allocations |
| wide | comparison, both states cached | 20 | 20 µs | 46 µs | 50 µs |  | 5 KiB in 48 allocations |
| wide | comparison, cancelled halfway | 5 | 3 µs | n/a | 13 µs |  |  |
| wide | load, cancelled halfway | 20 | 7 µs | 253 µs | 254 µs |  |  |
| wide | heap after a comparison | 1 |  |  |  | 22.7 MiB live |  |
| wide | heap while reloading | 1 |  |  |  | 44.6 MiB live |  |
| shuffled | inspect, cold file cache | 5 | 241.7 ms | n/a | 259.7 ms |  |  |
| shuffled | inspect | 5 | 259.1 ms | n/a | 274.1 ms | 265 MiB |  |
| shuffled | show reference | 5 | 447.8 ms | n/a | 486.8 ms | 570 MiB |  |
| shuffled | show root, cold file cache | 5 | 489.2 ms | n/a | 508.6 ms |  |  |
| shuffled | show root | 5 | 457.6 ms | n/a | 497.6 ms | 690 MiB |  |
| shuffled | diff | 5 | 1.41 s | n/a | 1.44 s | 784 MiB |  |
| shuffled | browser first state, cold file cache | 5 | 453.1 ms | n/a | 486.3 ms |  |  |
| shuffled | browser first state | 5 | 419.9 ms | n/a | 436.4 ms |  |  |
| shuffled | browser peak over comparison and two reloads | 5 |  |  |  | 923 MiB |  |
| shuffled | browser quit while comparing | 5 | 7.3 ms | n/a | 35.2 ms |  |  |
| shuffled | load | 5 | 308.7 ms | n/a | 335.6 ms |  | 681.2 MiB in 8000115 allocations |
| shuffled | preview, uncached | 5 | 53.7 ms | n/a | 61.2 ms |  | 244.4 MiB in 14 allocations |
| shuffled | preview, cached | 5 | 4 µs | n/a | 4 µs |  |  |
| shuffled | comparison, uncached | 5 | 1.01 s | n/a | 1.02 s |  | 1444.9 MiB in 429 allocations |
| shuffled | comparison, both states cached | 5 | 928.1 ms | n/a | 937.9 ms |  | 1078.3 MiB in 406 allocations |
| shuffled | comparison, cancelled halfway | 5 | 8.4 ms | n/a | 21.0 ms |  |  |
| shuffled | load, cancelled halfway | 5 | 8 µs | n/a | 14.2 ms |  |  |
| shuffled | heap after a comparison | 1 |  |  |  | 481.9 MiB live |  |
| shuffled | heap while reloading | 1 |  |  |  | 718.2 MiB live |  |
| repeated | inspect, cold file cache | 5 | 70.2 ms | n/a | 71.9 ms |  |  |
| repeated | inspect | 5 | 66.4 ms | n/a | 80.9 ms | 81 MiB |  |
| repeated | show reference | 5 | 119.6 ms | n/a | 127.2 ms | 155 MiB |  |
| repeated | show root, cold file cache | 5 | 137.6 ms | n/a | 143.0 ms |  |  |
| repeated | show root | 5 | 139.7 ms | n/a | 142.0 ms | 173 MiB |  |
| repeated | diff | 5 | 317.3 ms | n/a | 336.3 ms | 244 MiB |  |
| repeated | browser first state, cold file cache | 5 | 136.1 ms | n/a | 136.7 ms |  |  |
| repeated | browser first state | 5 | 119.5 ms | n/a | 120.1 ms |  |  |
| repeated | browser peak over comparison and two reloads | 5 |  |  |  | 368 MiB |  |
| repeated | browser quit while comparing | 5 | 3.4 ms | n/a | 4.4 ms |  |  |
| repeated | load | 5 | 72.6 ms | n/a | 76.8 ms |  | 155.6 MiB in 2000108 allocations |
| repeated | preview, uncached | 5 | 12.8 ms | n/a | 16.0 ms |  | 61.1 MiB in 15 allocations |
| repeated | preview, cached | 5 | 3 µs | n/a | 3 µs |  |  |
| repeated | comparison, uncached | 5 | 163.7 ms | n/a | 189.9 ms |  | 241.6 MiB in 206 allocations |
| repeated | comparison, both states cached | 5 | 147.0 ms | n/a | 168.7 ms |  | 149.9 MiB in 182 allocations |
| repeated | comparison, cancelled halfway | 5 | 22 µs | n/a | 26 µs |  |  |
| repeated | load, cancelled halfway | 5 | 9 µs | n/a | 3.8 ms |  |  |
| repeated | heap after a comparison | 1 |  |  |  | 117.0 MiB live |  |
| repeated | heap while reloading | 1 |  |  |  | 172.1 MiB live |  |
| replaced | inspect, cold file cache | 5 | 73.1 ms | n/a | 75.3 ms |  |  |
| replaced | inspect | 5 | 74.7 ms | n/a | 76.6 ms | 72 MiB |  |
| replaced | show reference | 5 | 125.7 ms | n/a | 127.7 ms | 170 MiB |  |
| replaced | show root, cold file cache | 5 | 135.1 ms | n/a | 151.3 ms |  |  |
| replaced | show root | 5 | 128.0 ms | n/a | 132.7 ms | 173 MiB |  |
| replaced | diff | 5 | 226.0 ms | n/a | 240.8 ms | 325 MiB |  |
| replaced | browser first state, cold file cache | 5 | 136.2 ms | n/a | 136.5 ms |  |  |
| replaced | browser first state | 5 | 119.7 ms | n/a | 136.5 ms |  |  |
| replaced | browser peak over comparison and two reloads | 5 |  |  |  | 516 MiB |  |
| replaced | browser quit while comparing | 5 | 2.1 ms | n/a | 2.4 ms |  |  |
| replaced | load | 5 | 75.3 ms | n/a | 76.3 ms |  | 175.0 MiB in 2000110 allocations |
| replaced | preview, uncached | 5 | 12.3 ms | n/a | 13.3 ms |  | 61.1 MiB in 14 allocations |
| replaced | preview, cached | 5 | 3 µs | n/a | 4 µs |  |  |
| replaced | comparison, uncached | 5 | 97.1 ms | n/a | 113.2 ms |  | 614.6 MiB in 339 allocations |
| replaced | comparison, both states cached | 5 | 73.8 ms | n/a | 80.1 ms |  | 522.9 MiB in 314 allocations |
| replaced | comparison, cancelled halfway | 5 | 26 µs | n/a | 32 µs |  |  |
| replaced | load, cancelled halfway | 5 | 862 µs | n/a | 2.1 ms |  |  |
| replaced | heap after a comparison | 1 |  |  |  | 160.8 MiB live |  |
| replaced | heap while reloading | 1 |  |  |  | 221.3 MiB live |  |
| changes-limit | inspect, cold file cache | 5 | 546.6 ms | n/a | 566.6 ms |  |  |
| changes-limit | inspect | 5 | 534.9 ms | n/a | 538.2 ms | 338 MiB |  |
| changes-limit | show reference | 5 | 433.9 ms | n/a | 461.1 ms | 338 MiB |  |
| changes-limit | show root, cold file cache | 5 | 513.5 ms | n/a | 536.8 ms |  |  |
| changes-limit | show root | 5 | 463.3 ms | n/a | 541.0 ms | 359 MiB |  |
| changes-limit | diff | 5 | 460.3 ms | n/a | 498.3 ms | 346 MiB |  |
| changes-limit | browser first state, cold file cache | 5 | 486.4 ms | n/a | 520.0 ms |  |  |
| changes-limit | browser first state | 5 | 437.0 ms | n/a | 470.4 ms |  |  |
| changes-limit | browser peak over comparison and two reloads | 5 |  |  |  | 755 MiB |  |
| changes-limit | browser quit while comparing | 5 | 3.2 ms | n/a | 4.3 ms |  |  |
| changes-limit | load | 5 | 405.8 ms | n/a | 437.4 ms |  | 980.9 MiB in 7842207 allocations |
| changes-limit | preview, uncached | 5 | 100.7 ms | n/a | 114.3 ms |  | 34.3 MiB in 560047 allocations |
| changes-limit | preview, cached | 5 | 4 µs | n/a | 5 µs |  |  |
| changes-limit | comparison, uncached | 5 | 111.8 ms | n/a | 117.3 ms |  | 34.9 MiB in 560084 allocations |
| changes-limit | comparison, both states cached | 5 | 47 µs | n/a | 55 µs |  | 8 KiB in 29 allocations |
| changes-limit | comparison, cancelled halfway | 5 | 11 µs | n/a | 19 µs |  |  |
| changes-limit | load, cancelled halfway | 5 | 7 µs | n/a | 17 µs |  |  |
| changes-limit | heap after a comparison | 1 |  |  |  | 203.8 MiB live |  |
| changes-limit | heap while reloading | 1 |  |  |  | 406.7 MiB live |  |
| entries-limit | inspect, cold file cache | 5 | 89.2 ms | n/a | 93.4 ms |  |  |
| entries-limit | inspect | 5 | 84.2 ms | n/a | 86.6 ms | 161 MiB |  |
| entries-limit | show reference | 5 | 306.1 ms | n/a | 315.6 ms | 419 MiB |  |
| entries-limit | show root, cold file cache | 5 | 391.7 ms | n/a | 434.8 ms |  |  |
| entries-limit | show root | 5 | 365.8 ms | n/a | 436.6 ms | 532 MiB |  |
| entries-limit | diff | 5 | 406.7 ms | n/a | 421.4 ms | 634 MiB |  |
| entries-limit | browser first state, cold file cache | 5 | 269.9 ms | n/a | 286.5 ms |  |  |
| entries-limit | browser first state | 5 | 270.0 ms | n/a | 270.5 ms |  |  |
| entries-limit | browser peak over comparison and two reloads | 5 |  |  |  | 765 MiB |  |
| entries-limit | browser quit while comparing | 5 | 2.9 ms | n/a | 3.8 ms |  |  |
| entries-limit | load | 5 | 182.5 ms | n/a | 185.3 ms |  | 463.2 MiB in 5000104 allocations |
| entries-limit | preview, uncached | 5 | 133.0 ms | n/a | 135.2 ms |  | 358.1 MiB in 27745 allocations |
| entries-limit | preview, cached | 5 | 4 µs | n/a | 5 µs |  |  |
| entries-limit | comparison, uncached | 5 | 200.1 ms | n/a | 210.3 ms |  | 587.1 MiB in 27766 allocations |
| entries-limit | comparison, both states cached | 5 | 12.2 ms | n/a | 13.5 ms |  | 106.8 MiB in 13 allocations |
| entries-limit | comparison, cancelled halfway | 5 | 7 µs | n/a | 8 µs |  |  |
| entries-limit | load, cancelled halfway | 5 | 14.8 ms | n/a | 18.5 ms |  |  |
| entries-limit | heap after a comparison | 1 |  |  |  | 366.5 MiB live |  |
| entries-limit | heap while reloading | 1 |  |  |  | 518.5 MiB live |  |
| lines-limit | inspect, cold file cache | 5 | 14.6 ms | n/a | 16.0 ms |  |  |
| lines-limit | inspect | 5 | 12.0 ms | n/a | 12.9 ms | 68 MiB |  |
| lines-limit | show reference | 5 | 58.7 ms | n/a | 59.9 ms | 191 MiB |  |
| lines-limit | show root, cold file cache | 5 | 66.6 ms | n/a | 68.3 ms |  |  |
| lines-limit | show root | 5 | 59.2 ms | n/a | 61.4 ms | 200 MiB |  |
| lines-limit | diff | 5 | 85.0 ms | n/a | 86.2 ms | 224 MiB |  |
| lines-limit | browser first state, cold file cache | 5 | 186.7 ms | n/a | 203.2 ms |  |  |
| lines-limit | browser first state | 5 | 186.6 ms | n/a | 187.0 ms |  |  |
| lines-limit | browser peak over comparison and two reloads | 5 |  |  |  | 306 MiB |  |
| lines-limit | browser quit while comparing | 5 | 2.5 ms | n/a | 2.9 ms |  |  |
| lines-limit | load | 5 | 44.2 ms | n/a | 44.8 ms |  | 243.2 MiB in 98 allocations |
| lines-limit | preview, uncached | 5 | 110.1 ms | n/a | 118.1 ms |  | 241 KiB in 27 allocations |
| lines-limit | preview, cached | 5 | 2 µs | n/a | 5 µs |  |  |
| lines-limit | comparison, uncached | 5 | 218.7 ms | n/a | 219.0 ms |  | 483 KiB in 73 allocations |
| lines-limit | comparison, both states cached | 5 | 2.5 ms | n/a | 2.7 ms |  | 1 KiB in 22 allocations |
| lines-limit | comparison, cancelled halfway | 5 | 219 µs | n/a | 441 µs |  |  |
| lines-limit | load, cancelled halfway | 5 | 18.6 ms | n/a | 20.0 ms |  |  |
| lines-limit | heap after a comparison | 1 |  |  |  | 68.4 MiB live |  |
| lines-limit | heap while reloading | 1 |  |  |  | 135.6 MiB live |  |
