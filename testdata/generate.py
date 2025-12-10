import csv

# --- Configuration Parameters ---
# Number of alerts to generate
NUM_ALERTS = 1000
# Output file name
OUTPUT_FILE = 'synthetic_alerts_loadtest_sequential.csv'
# The fields/columns for the final CSV file
FIELDNAMES = ['start', 'end', 'alertname', 'namespace', 'severity', 'silenced', 'labels']

# Time parameters
BASE_START_TIME = 3000
START_INCREMENT = 1  # Each subsequent alert starts 1 unit later (e.g., 3000, 3001, 3002, ...)
FIXED_END_TIME = 4000 # All alerts are assumed to be active until this time

# Template values for the simulated failure (non-time fields)
template_data = {
    'namespace': 'openshift-monitoring',
    'severity': 'warning',
    'silenced': False,
    'labels': '{"name": "monitoring"}'
}

# --- Data Generation ---

# 1. Prepare the list to hold all alert dictionaries
alerts_list = []

# 2. Generate the alerts
for i in range(NUM_ALERTS):
    # Calculate sequential start time: Base + (Index * Increment)
    current_start = BASE_START_TIME + (i * START_INCREMENT)
    
    alert_name = f'LoadTestAlert-{i+1}'
    
    # Create a dictionary for the current alert
    alert_row = {
        'start': current_start,
        'end': FIXED_END_TIME,
        'alertname': alert_name,
        'namespace': template_data['namespace'],
        'severity': template_data['severity'],
        'silenced': template_data['silenced'],
        'labels': template_data['labels']
    }
    
    alerts_list.append(alert_row)

# --- Write to CSV File ---

try:
    # Use 'w' mode (write) and newline='' for consistent cross-platform CSV writing
    with open(OUTPUT_FILE, 'w', newline='') as csvfile:
        # Create the DictWriter object
        writer = csv.DictWriter(csvfile, fieldnames=FIELDNAMES)
        
        # Write the header row
        writer.writeheader()
        
        # Write all the data rows
        writer.writerows(alerts_list)

    print(f"Successfully generated {NUM_ALERTS} alerts into {OUTPUT_FILE}")
    print("\nFirst 5 entries:")
    # Displaying the first few generated dictionaries for confirmation
    for i in range(min(5, NUM_ALERTS)):
        print(alerts_list[i])

except IOError:
    print(f"Error: Could not write to file {OUTPUT_FILE}. Check file permissions.")
