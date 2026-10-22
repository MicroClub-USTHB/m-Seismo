fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_prost_build::configure()
        // XXX: generated AgentClient::connect clashes with the Connect rpc,
        // servers only for now
        .build_client(false)
        .compile_protos(
            &[
                "../spec/proto/seismo/v1/agent.proto",
                "../spec/proto/seismo/v1/coordinator.proto",
            ],
            &["../spec/proto"],
        )?;
    Ok(())
}
