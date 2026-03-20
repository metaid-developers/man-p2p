FROM ubuntu:24.04
# 设置工作目录
WORKDIR /man
# 替换为阿里云镜像源（一次性替换所有 archive.ubuntu.com）
RUN sed -i 's/archive.ubuntu.com/mirrors.aliyun.com/g' /etc/apt/sources.list
# 合并 apt-get 操作，减少层数并清理缓存
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        wget \
        curl \
        libc6 \
        libzmq3-dev \
        g++ && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# 验证
RUN strings /usr/lib/x86_64-linux-gnu/libstdc++.so.6 | grep GLIBCXX

# 设置默认环境变量
ENV CHAIN=btc,mvc
ENV TEST=0

# 设置启动命令
CMD /man/manindexer -test=${TEST} -chain=${CHAIN}
# 构建命令示例：
# docker build -t man_indexer_v2:0.1 .
# 运行命令示例：
# docker run -e CHAIN=mvc,btc -e TEST=0 -d --memory=6g --memory-swap=6g --name man-indexer-v2 -v /mnt/man_v2:/man --network host --restart=always man_indexer_v2:0.1