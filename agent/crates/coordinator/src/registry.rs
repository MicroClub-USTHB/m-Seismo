use crate::config;
use seismo_proto::{self as pb, agent_message::Message};
use std::{
    collections::HashMap,
    sync::{
        Arc, Mutex,
        atomic::{AtomicU64, Ordering},
    },
};
use tokio::sync::{mpsc, oneshot};
use tonic::Status;

pub const PROTOCOL_VERSION: u32 = 1;

/// What an agent answered to one query.
pub enum Reply {
    Result(pb::QueryResult),
    Error(pb::QueryError),
    Metrics(pb::MetricsResult),
}

pub type Outbound = mpsc::Sender<Result<pb::CoordinatorMessage, Status>>;

/// Queries sent to one agent and not answered yet, by query id.
pub(crate) type Pending = Arc<Mutex<HashMap<String, oneshot::Sender<Reply>>>>;

/// What fan-out needs to talk to one agent. Cheap to clone, so we can query
/// without holding the registry lock.
#[derive(Clone)]
pub(crate) struct AgentHandle {
    pub id: String,
    pub source: i32,
    pub services: Vec<String>,
    pub tx: Outbound,
    pub pending: Pending,
}

struct Entry {
    conn: u64,
    handle: AgentHandle,
}

/// The live agents. In memory only, an agent that disconnects is gone.
pub struct Registry {
    pub(crate) coordinator_id: String,
    pub(crate) query: config::Query,
    agents: Mutex<HashMap<String, Entry>>,
    next_conn: AtomicU64,
    pub(crate) next_query: AtomicU64,
}

/// Proof of registration, what a connection holds on to.
pub struct Conn {
    agent_id: String,
    conn: u64,
    pending: Pending,
}

impl Registry {
    pub fn new(coordinator_id: String, query: config::Query) -> Self {
        Self {
            coordinator_id,
            query,
            agents: Mutex::default(),
            next_conn: AtomicU64::new(1),
            next_query: AtomicU64::new(1),
        }
    }

    pub fn len(&self) -> usize {
        self.agents.lock().unwrap().len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Rejections are plain gRPC statuses, the protocol has no ack message.
    pub fn register(&self, r: pb::Register, tx: Outbound) -> Result<Conn, Status> {
        if r.protocol_version != PROTOCOL_VERSION {
            return Err(Status::failed_precondition(format!(
                "protocol_version {} not supported, expected {PROTOCOL_VERSION}",
                r.protocol_version
            )));
        }
        if r.agent_id.is_empty() {
            return Err(Status::invalid_argument("agent_id must not be empty"));
        }
        let mut agents = self.agents.lock().unwrap();
        if agents.contains_key(&r.agent_id) {
            return Err(Status::already_exists(format!(
                "another live stream already uses agent_id {}",
                r.agent_id
            )));
        }
        let conn = self.next_conn.fetch_add(1, Ordering::Relaxed);
        let pending = Pending::default();
        let handle = AgentHandle {
            id: r.agent_id.clone(),
            source: r.source,
            services: r.services,
            tx,
            pending: pending.clone(),
        };
        agents.insert(r.agent_id.clone(), Entry { conn, handle });
        Ok(Conn {
            agent_id: r.agent_id,
            conn,
            pending,
        })
    }

    /// Routes an answer to whoever waits for it. One for a query that was
    /// cancelled, timed out or never existed is dropped, as the protocol says.
    pub fn deliver(&self, conn: &Conn, msg: Message) {
        let (id, reply) = match msg {
            Message::Result(r) => (r.query_id.clone(), Reply::Result(r)),
            Message::Error(e) => (e.query_id.clone(), Reply::Error(e)),
            Message::MetricsResult(r) => (r.query_id.clone(), Reply::Metrics(r)),
            Message::Register(_) => return,
        };
        if let Some(waiter) = conn.pending.lock().unwrap().remove(&id) {
            let _ = waiter.send(reply);
        }
    }

    /// Drops the agent. Whoever still waits for it sees the sender go away,
    /// which fan-out reports as DISCONNECTED.
    pub fn deregister(&self, conn: &Conn) {
        let mut agents = self.agents.lock().unwrap();
        // a newer connection may already own this id
        if agents
            .get(&conn.agent_id)
            .is_some_and(|e| e.conn == conn.conn)
        {
            agents.remove(&conn.agent_id);
        }
        conn.pending.lock().unwrap().clear();
    }

    pub(crate) fn snapshot(&self) -> Vec<AgentHandle> {
        let mut v: Vec<_> = self
            .agents
            .lock()
            .unwrap()
            .values()
            .map(|e| e.handle.clone())
            .collect();
        v.sort_by(|a, b| a.id.cmp(&b.id));
        v
    }
}
