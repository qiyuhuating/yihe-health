use serde::Serialize;
use serde_json::{json, Value};
use std::{
    collections::VecDeque,
    fs::{File, OpenOptions},
    io::{BufWriter, Write},
    path::{Path, PathBuf},
};
use watch_sim::{
    protocol::{HealthSampleData, ReportPayload},
    scenario::Scenario,
};

pub struct LocalRun {
    directory: PathBuf,
    records: BufWriter<File>,
    events: BufWriter<File>,
    history: VecDeque<Value>,
    recent_events: VecDeque<Value>,
    previous_scene: Option<Scenario>,
    samples: u64,
    event_count: u64,
}

impl LocalRun {
    pub fn new(directory: &Path) -> Result<Self, String> {
        let directory = directory.join(format!("run-{}", uuid::Uuid::new_v4().simple()));
        std::fs::create_dir_all(&directory).map_err(|e| format!("无法创建本地目录：{e}"))?;
        let open = |name: &str| {
            OpenOptions::new()
                .create_new(true)
                .write(true)
                .open(directory.join(name))
        };
        let records = BufWriter::new(open("records.jsonl").map_err(|e| e.to_string())?);
        let events = BufWriter::new(open("events.jsonl").map_err(|e| e.to_string())?);
        println!("本地运行：{}", directory.join("index.html").display());
        Ok(Self {
            directory,
            records,
            events,
            history: VecDeque::with_capacity(120),
            recent_events: VecDeque::with_capacity(20),
            previous_scene: None,
            samples: 0,
            event_count: 0,
        })
    }

    pub fn report(&mut self, payload: &ReportPayload, scene: Scenario) -> Result<(), String> {
        let mut data = serde_json::to_value(payload).map_err(|e| e.to_string())?;
        data.as_object_mut()
            .expect("report 是对象")
            .remove("secret");
        self.samples += 1;
        let frame = json!({"type":"report","mode":"local","scenario":scene.code(),"scenarioLabel":scene.label(),"sampleCount":self.samples,"data":data});
        json_line(&mut self.records, &frame)?;
        if self.previous_scene != Some(scene) && (scene.asks_sos() || scene.emits_alarm()) {
            self.event_count += 1;
            let event = json!({"kind":scene.code(),"eventId":uuid::Uuid::new_v4().simple().to_string(),"at":payload.be_time,"lng":payload.lng,"lat":payload.lat,"mode":"local"});
            json_line(&mut self.events, &event)?;
            if self.recent_events.len() == 20 {
                self.recent_events.pop_front();
            }
            self.recent_events.push_back(event);
        }
        self.previous_scene = Some(scene);
        if self.history.len() == 120 {
            self.history.pop_front();
        }
        self.history.push_back(json!({"at":payload.be_time,"hr":payload.hr,"bp":payload.bp,"lng":payload.lng,"lat":payload.lat}));
        let mut latest = frame;
        latest["eventCount"] = json!(self.event_count);
        write_snapshot(
            &self.directory.join("latest.json"),
            &serde_json::to_vec(&latest).map_err(|e| e.to_string())?,
        )?;
        let view = json!({"latest":latest,"history":self.history,"events":self.recent_events});
        let embedded = serde_json::to_string(&view)
            .map_err(|e| e.to_string())?
            .replace('<', "\\u003c");
        let page = include_str!("../assets/local.html").replace("__LOCAL_DATA__", &embedded);
        write_snapshot(&self.directory.join("index.html"), page.as_bytes())?;
        Ok(())
    }

    pub fn health_samples<S: Serialize, R: Serialize>(
        &mut self,
        samples: &[HealthSampleData<S, R>],
    ) -> Result<(), String> {
        json_line(
            &mut self.records,
            &json!({"type":"healthBatch","mode":"local","samples":samples}),
        )
    }
}

fn json_line(writer: &mut BufWriter<File>, value: &Value) -> Result<(), String> {
    serde_json::to_writer(&mut *writer, value).map_err(|e| format!("写入本地数据失败：{e}"))?;
    writer
        .write_all(b"\n")
        .and_then(|_| writer.flush())
        .map_err(|e| format!("写入本地数据失败：{e}"))
}

fn write_snapshot(path: &Path, bytes: &[u8]) -> Result<(), String> {
    let temporary = path.with_extension("tmp");
    std::fs::write(&temporary, bytes)
        .and_then(|_| std::fs::rename(&temporary, path))
        .map_err(|e| format!("更新本地页面失败：{e}"))
}
