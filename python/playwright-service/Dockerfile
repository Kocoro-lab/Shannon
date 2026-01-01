FROM python:3.11-slim

WORKDIR /app

# Install system dependencies for Chromium
RUN apt-get update && apt-get install -y --no-install-recommends \
    libasound2 \
    libatk-bridge2.0-0 \
    libatk1.0-0 \
    libcups2 \
    libdbus-1-3 \
    libdrm2 \
    libgbm1 \
    libgtk-3-0 \
    libnspr4 \
    libnss3 \
    libx11-xcb1 \
    libxcomposite1 \
    libxdamage1 \
    libxfixes3 \
    libxkbcommon0 \
    libxrandr2 \
    fonts-liberation \
    fonts-noto-cjk \
    xvfb \
    && rm -rf /var/lib/apt/lists/*

# Install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Set browser path to shared location
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/playwright

# Install Playwright Chromium to shared path
RUN mkdir -p /opt/playwright && playwright install chromium

# Copy application code
COPY . .

# Non-root user for security
RUN useradd -m -u 1000 appuser && chown -R appuser:appuser /app /opt/playwright
USER appuser

EXPOSE 8002

CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8002"]
