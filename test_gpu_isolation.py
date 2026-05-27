import os, sys, time

print("=== GPU Isolation Test ===")
print("PID:", os.getpid())
print("CUDA_VISIBLE_DEVICES:", os.environ.get("CUDA_VISIBLE_DEVICES", "NOT SET"))
print("ROCR_VISIBLE_DEVICES:", os.environ.get("ROCR_VISIBLE_DEVICES", "NOT SET"))
print("All env vars with GPU:")
for k, v in sorted(os.environ.items()):
    if 'GPU' in k.upper() or 'CUDA' in k.upper() or 'ROCR' in k.upper() or 'VISIBLE' in k.upper():
        print(f"  {k}={v}")
print("=== End ===")
sys.stdout.flush()
time.sleep(2)
print("done")
sys.stdout.flush()
