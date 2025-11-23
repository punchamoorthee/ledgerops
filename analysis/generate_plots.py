import matplotlib.pyplot as plt
import numpy as np

# Data from your k6 runs (Turn 13)
scenarios = ['Uniform (Baseline)', 'Hot-Spot (Stress)']
p95_latency = [82.12, 75.77]  # ms
abort_rates = [2.40, 87.07]   # percentage

# Setup
x = np.arange(len(scenarios))
width = 0.6
color_base = '#40826D'  # Your project's primary color

# --- Figure 6.1: Latency Distribution ---
fig1, ax1 = plt.subplots(figsize=(8, 5))
rects1 = ax1.bar(x, p95_latency, width, color=color_base)

ax1.set_ylabel('p95 Latency (ms)')
# ax1.set_title('Figure 6.1: Latency Performance (10 Concurrent Users)') # REMOVED
ax1.set_xticks(x)
ax1.set_xticklabels(scenarios)
ax1.set_ylim(0, 100) 

# Add labels on top
ax1.bar_label(rects1, padding=3, fmt='%.2f ms')

plt.tight_layout()
plt.savefig('fig06_latency.png')
print("Generated fig06_latency.png")

# --- Figure 6.2: Abort Rate ---
fig2, ax2 = plt.subplots(figsize=(8, 5))
rects2 = ax2.bar(x, abort_rates, width, color='#D9534F') # Red for aborts

ax2.set_ylabel('Abort Rate (%)')
# ax2.set_title('Figure 6.2: Transaction Abort Rate (10 Concurrent Users)') # REMOVED
ax2.set_xticks(x)
ax2.set_xticklabels(scenarios)
ax2.set_ylim(0, 100)

# Add labels on top
ax2.bar_label(rects2, padding=3, fmt='%.2f%%')

plt.tight_layout()
plt.savefig('fig07_abort.png')
print("Generated fig07_abort.png")