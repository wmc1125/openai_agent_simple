const response = await fetch('http://127.0.0.1:8080/v1/chat/completions', {
    method: 'POST',
    headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${apiKey}`,
    },
    body: JSON.stringify(requestBody),
});