use seismo_proto::agent_server::{Agent, AgentServer};
use seismo_proto::{AgentMessage, CoordinatorMessage};
use tonic::codegen::BoxStream;
use tonic::{Request, Response, Status, Streaming, transport::Server};

struct Service;

#[tonic::async_trait]
impl Agent for Service {
    type ConnectStream = BoxStream<CoordinatorMessage>;

    async fn connect(
        &self,
        _req: Request<Streaming<AgentMessage>>,
    ) -> Result<Response<Self::ConnectStream>, Status> {
        unimplemented!()
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr = std::env::var("SEISMO_LISTEN")
        .unwrap_or_else(|_| "[::]:50051".into())
        .parse()?;
    Server::builder()
        .add_service(AgentServer::new(Service))
        .serve(addr)
        .await?;
    Ok(())
}
