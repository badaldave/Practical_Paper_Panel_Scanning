import multiprocessing
import psutil

print("CPUs available:", multiprocessing.cpu_count())
mem = psutil.virtual_memory()
print(f"Total Memory: {mem.total / (1024**3):.2f} GB")
print(f"Available Memory: {mem.available / (1024**3):.2f} GB")
