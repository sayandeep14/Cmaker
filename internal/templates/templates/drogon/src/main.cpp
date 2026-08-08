#include <drogon/drogon.h>

int main() {
    drogon::app().registerHandler(
        "/",
        [](const drogon::HttpRequestPtr &,
           std::function<void(const drogon::HttpResponsePtr &)> &&callback) {
            auto resp = drogon::HttpResponse::newHttpResponse();
            resp->setBody("Hello from cmaker (drogon template)!\n");
            callback(resp);
        });

    drogon::app().addListener("127.0.0.1", 8080).run();
    return 0;
}
