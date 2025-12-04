import matplotlib.pyplot as plt
import numpy as np

# --- DATA FROM YOUR GOLDEN RUN (Dec 2, 2025) ---
scenarios = ['Uniform (10 VU)', 'Hot-Spot (50 VU)']
p95_latency = [39.88, 145.27]   # ms
abort_rates = [1.79, 87.40]     # percentage
throughput  = [326.5, 527.2]    # RPS (Requests Per Second)

# --- SETUP ---
x = np.arange(len(scenarios))
width = 0.6
color_primary = '#40826D'  # CSUDH Green
color_stress  = '#D9534F'  # Red for aborts
color_scale   = '#337AB7'  # Blue for scalability

# Use a clean style
plt.style.use('default')

# --- FIGURE 5.1: Latency Distribution ---
fig1, ax1 = plt.subplots(figsize=(8, 5))
rects1 = ax1.bar(x, p95_latency, width, color=color_primary)

ax1.set_ylabel('p95 Latency (ms)')
ax1.set_xticks(x)
ax1.set_xticklabels(scenarios)
ax1.set_ylim(0, 180) # Adjusted for your 145ms result
ax1.grid(axis='y', linestyle='--', alpha=0.3)

# Add labels on top
ax1.bar_label(rects1, padding=3, fmt='%.2f ms')

plt.tight_layout()
plt.savefig('fig06_latency.png', dpi=300)
print("Generated fig06_latency.png")

# --- FIGURE 5.2: Abort Rate ---
fig2, ax2 = plt.subplots(figsize=(8, 5))
rects2 = ax2.bar(x, abort_rates, width, color=color_stress)

ax2.set_ylabel('Abort Rate (%)')
ax2.set_xticks(x)
ax2.set_xticklabels(scenarios)
ax2.set_ylim(0, 100)
ax2.grid(axis='y', linestyle='--', alpha=0.3)

# Add labels on top
ax2.bar_label(rects2, padding=3, fmt='%.2f%%')

plt.tight_layout()
plt.savefig('fig07_abort.png', dpi=300)
print("Generated fig07_abort.png")

# --- FIGURE 5.3: Throughput Scalability ---
# This plots the RPS increase as we moved from 10 VUs to 50 VUs
fig3, ax3 = plt.subplots(figsize=(8, 5))
rects3 = ax3.plot(scenarios, throughput, marker='o', linestyle='-', color=color_scale, linewidth=2, markersize=8)

ax3.set_ylabel('Throughput (Req/Sec)')
ax3.set_ylim(0, 600)
ax3.grid(True, linestyle='--', alpha=0.3)

# Annotate points
for i, txt in enumerate(throughput):
    ax3.annotate(f"{txt} RPS", (scenarios[i], throughput[i]), 
                 xytext=(0, 10), textcoords='offset points', ha='center')

plt.tight_layout()
plt.savefig('fig08_throughput.png', dpi=300)
print("Generated fig08_throughput.png")