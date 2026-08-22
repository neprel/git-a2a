defmodule Consumer.MixProject do
  use Mix.Project

  def project do
    [app: :consumer, version: "1.0.0", deps: deps()]
  end

  # keep this list and comment
  defp deps do
    [
      {:jason, "~> 1.4"},
    ]
  end
end
