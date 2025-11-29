#!/usr/bin/env python3
import time
from datetime import datetime

def main():
    print("Hello from dzjjy deployment!")
    print(f"Application started at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")

    # 保持运行
    while True:
        print(f"Running... {datetime.now().strftime('%H:%M:%S')}")
        time.sleep(5)

if __name__ == "__main__":
    main()
