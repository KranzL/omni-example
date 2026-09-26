200 random orders per row. Start: 36 seeds (hard -> mid). Label from always-cheap outcome.

kNN5 emb, static (no learning)
| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |
|---|---|---|---|---|---|---|---|---|
| 4 | 0.695 | 4.00/4 | 0.00 | $0.0338 | $0.0445 | 0.746 | 0.686 | 1.000 |
| 8 | 0.704 | 8.00/8 | 0.00 | $0.0680 | $0.0896 | 0.754 | 0.682 | 1.000 |
| 16 | 0.689 | 16.00/16 | 0.00 | $0.1376 | $0.1804 | 0.760 | 0.686 | 1.000 |
| 24 | 0.683 | 24.00/24 | 0.00 | $0.2089 | $0.2727 | 0.765 | 0.702 | 1.000 |
| 32 | 0.688 | 32.00/32 | 0.00 | $0.2768 | $0.3631 | 0.762 | - | - |

kNN5 emb, free labels (eval labels every question)
| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |
|---|---|---|---|---|---|---|---|---|
| 4 | 0.723 | 4.00/4 | 0.00 | $0.0333 | $0.0445 | 0.734 | 0.764 | 0.995 |
| 8 | 0.761 | 7.99/8 | 0.01 | $0.0660 | $0.0896 | 0.731 | 0.804 | 0.946 |
| 16 | 0.783 | 15.76/16 | 0.21 | $0.1295 | $0.1804 | 0.715 | 0.819 | 0.778 |
| 24 | 0.791 | 23.33/24 | 0.48 | $0.1925 | $0.2727 | 0.705 | 0.831 | 0.692 |
| 32 | 0.803 | 30.98/32 | 0.66 | $0.2523 | $0.3631 | 0.695 | - | - |

kNN5 emb, shadow 0.25
| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |
|---|---|---|---|---|---|---|---|---|
| 4 | 0.706 | 4.00/4 | 0.00 | $0.0443 | $0.0454 | 0.961 | 0.709 | 1.000 |
| 8 | 0.713 | 8.00/8 | 0.00 | $0.0896 | $0.0912 | 0.977 | 0.725 | 0.995 |
| 16 | 0.726 | 16.00/16 | 0.00 | $0.1789 | $0.1822 | 0.979 | 0.765 | 0.988 |
| 24 | 0.743 | 23.99/24 | 0.01 | $0.2647 | $0.2726 | 0.970 | 0.786 | 0.956 |
| 32 | 0.755 | 31.91/32 | 0.07 | $0.3520 | $0.3631 | 0.969 | - | - |

kNN5 emb, shadow 0.5
| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |
|---|---|---|---|---|---|---|---|---|
| 4 | 0.716 | 4.00/4 | 0.00 | $0.0528 | $0.0454 | 1.148 | 0.728 | 1.000 |
| 8 | 0.733 | 8.00/8 | 0.00 | $0.1084 | $0.0912 | 1.183 | 0.761 | 0.987 |
| 16 | 0.761 | 15.99/16 | 0.01 | $0.2175 | $0.1822 | 1.190 | 0.807 | 0.947 |
| 24 | 0.777 | 23.91/24 | 0.08 | $0.3233 | $0.2726 | 1.184 | 0.816 | 0.818 |
| 32 | 0.786 | 31.73/32 | 0.21 | $0.4320 | $0.3631 | 1.190 | - | - |

kNN5 emb, shadow 1.0
| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |
|---|---|---|---|---|---|---|---|---|
| 4 | 0.723 | 4.00/4 | 0.00 | $0.0737 | $0.0445 | 1.639 | 0.764 | 0.995 |
| 8 | 0.761 | 8.00/8 | 0.00 | $0.1488 | $0.0896 | 1.653 | 0.804 | 0.946 |
| 16 | 0.783 | 16.00/16 | 0.00 | $0.3032 | $0.1804 | 1.677 | 0.819 | 0.778 |
| 24 | 0.791 | 24.00/24 | 0.00 | $0.4595 | $0.2727 | 1.684 | 0.831 | 0.692 |
| 32 | 0.803 | 32.00/32 | 0.00 | $0.6107 | $0.3631 | 1.682 | - | - |

LR heur, static (no learning)
| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |
|---|---|---|---|---|---|---|---|---|
| 4 | 0.695 | 4.00/4 | 0.00 | $0.0337 | $0.0445 | 0.743 | 0.651 | 1.000 |
| 8 | 0.681 | 8.00/8 | 0.00 | $0.0687 | $0.0896 | 0.761 | 0.648 | 1.000 |
| 16 | 0.664 | 16.00/16 | 0.00 | $0.1390 | $0.1804 | 0.768 | 0.648 | 1.000 |
| 24 | 0.657 | 24.00/24 | 0.00 | $0.2111 | $0.2727 | 0.773 | 0.655 | 1.000 |
| 32 | 0.656 | 32.00/32 | 0.00 | $0.2807 | $0.3631 | 0.773 | - | - |

LR heur, free labels (eval labels every question)
| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |
|---|---|---|---|---|---|---|---|---|
| 4 | 0.710 | 4.00/4 | 0.00 | $0.0334 | $0.0445 | 0.736 | 0.715 | 1.000 |
| 8 | 0.724 | 8.00/8 | 0.00 | $0.0669 | $0.0896 | 0.741 | 0.751 | 1.000 |
| 16 | 0.741 | 16.00/16 | 0.00 | $0.1326 | $0.1804 | 0.733 | 0.780 | 1.000 |
| 24 | 0.752 | 24.00/24 | 0.00 | $0.1987 | $0.2727 | 0.728 | 0.787 | 1.000 |
| 32 | 0.760 | 32.00/32 | 0.00 | $0.2627 | $0.3631 | 0.724 | - | - |

LR heur, shadow 0.25
| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |
|---|---|---|---|---|---|---|---|---|
| 4 | 0.674 | 4.00/4 | 0.00 | $0.0447 | $0.0454 | 0.970 | 0.672 | 1.000 |
| 8 | 0.674 | 8.00/8 | 0.00 | $0.0907 | $0.0912 | 0.989 | 0.687 | 1.000 |
| 16 | 0.689 | 16.00/16 | 0.00 | $0.1809 | $0.1822 | 0.990 | 0.723 | 1.000 |
| 24 | 0.701 | 24.00/24 | 0.00 | $0.2681 | $0.2726 | 0.982 | 0.743 | 1.000 |
| 32 | 0.713 | 32.00/32 | 0.00 | $0.3567 | $0.3631 | 0.982 | - | - |

LR heur, shadow 0.5
| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |
|---|---|---|---|---|---|---|---|---|
| 4 | 0.676 | 4.00/4 | 0.00 | $0.0533 | $0.0454 | 1.159 | 0.689 | 1.000 |
| 8 | 0.682 | 8.00/8 | 0.00 | $0.1097 | $0.0912 | 1.197 | 0.715 | 1.000 |
| 16 | 0.708 | 16.00/16 | 0.00 | $0.2202 | $0.1822 | 1.205 | 0.756 | 1.000 |
| 24 | 0.725 | 24.00/24 | 0.00 | $0.3264 | $0.2726 | 1.196 | 0.779 | 1.000 |
| 32 | 0.739 | 32.00/32 | 0.00 | $0.4359 | $0.3631 | 1.200 | - | - |

LR heur, shadow 1.0
| questions seen | cumulative routing acc | mean correct | P(any wrong so far) | mean cumulative cost | always-mid on same questions | cost / always-mid | routing acc on unseen | mid recall on unseen |
|---|---|---|---|---|---|---|---|---|
| 4 | 0.710 | 4.00/4 | 0.00 | $0.0737 | $0.0445 | 1.639 | 0.715 | 1.000 |
| 8 | 0.724 | 8.00/8 | 0.00 | $0.1488 | $0.0896 | 1.653 | 0.751 | 1.000 |
| 16 | 0.741 | 16.00/16 | 0.00 | $0.3032 | $0.1804 | 1.677 | 0.780 | 1.000 |
| 24 | 0.752 | 24.00/24 | 0.00 | $0.4595 | $0.2727 | 1.684 | 0.787 | 1.000 |
| 32 | 0.760 | 32.00/32 | 0.00 | $0.6107 | $0.3631 | 1.682 | - | - |
