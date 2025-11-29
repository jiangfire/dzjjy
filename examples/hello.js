#!/usr/bin/env node

function formatTime(date) {
    return date.toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
    });
}

function main() {
    console.log("Hello from dzjjy deployment!");
    console.log(`Application started at: ${formatTime(new Date())}`);

    // 保持运行
    setInterval(() => {
        const now = new Date();
        const timeStr = now.toLocaleTimeString('zh-CN', { hour12: false });
        console.log(`Running... ${timeStr}`);
    }, 5000);
}

main();
