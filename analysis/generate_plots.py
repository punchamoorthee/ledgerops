import matplotlib.pyplot as plt
import json
import os

FILES = {
    'Uniform': 'results_uniform.json',
    'Hot-Spot': 'results_hotspot.json'
}

def load_data():
    scenarios = []
    tps = []
    abort_rates = []
    
    for label, filename in FILES.items():
        if not os.path.exists(filename):
            print(f"Warning: {filename} not found. Skipping.")
            continue
            
        with open(filename, 'r') as f:
            data = json.load(f)
            scenarios.append(label)
            tps.append(data.get('throughput_tps', 0))
            abort_rates.append(data.get('abort_rate_pct', 0))
            
    return scenarios, tps, abort_rates

def main():
    scenarios, tps, abort_rates = load_data()
    if not scenarios:
        print("No data found.")
        return

    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(12, 5))
    
    bars1 = ax1.bar(scenarios, tps, color=['#40826D', '#D9534F'])
    ax1.set_title('System Throughput')
    ax1.set_ylabel('Transactions Per Second (TPS)')
    ax1.bar_label(bars1, fmt='%.0f')

    bars2 = ax2.bar(scenarios, abort_rates, color=['#40826D', '#D9534F'])
    ax2.set_title('Transaction Abort Rate')
    ax2.set_ylabel('Abort Rate (%)')
    ax2.bar_label(bars2, fmt='%.1f%%')

    plt.tight_layout()
    plt.savefig('benchmark_results.png', dpi=300)
    print("Generated benchmark_results.png")

if __name__ == "__main__":
    main()
