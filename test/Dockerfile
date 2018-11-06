FROM alpine

ADD thread_busyloop.cpp .
RUN apk add g++ && g++ thread_busyloop.cpp -o /thread_busyloop -lpthread -static  && apk del g++

