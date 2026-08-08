#include <wx/wx.h>

class MyApp : public wxApp {
public:
    bool OnInit() override {
        wxFrame *frame = new wxFrame(nullptr, wxID_ANY, "Hello from cmaker (wxWidgets template)!");
        frame->Show(true);
        return true;
    }
};

wxIMPLEMENT_APP(MyApp);
