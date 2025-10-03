import pytest
import requests
import os
import json
import csv
import time
import uuid
from deepeval import assert_test
from deepeval.metrics import GEval, FaithfulnessMetric
from deepeval.test_case import LLMTestCase, LLMTestCaseParams
from prometheus_client import CollectorRegistry, Gauge, push_to_gateway

URL = "https://127.0.0.1:8080/v1/query"
LS_API_KEY = os.getenv("LS_API_KEY")

def test_ragas_faithfulness():
    input_query = "What is the status of the cluster? Provide a summary of firing incidents if any"
    # Create the faithfulness metric with a threshold of 0.7 for high accuracy
    faithfulness_metric = FaithfulnessMetric(threshold=0.7)
    # Get the actual response from the LLM
    actual_output = get_lightspeed_response(input_query)
    # Define retrieval context - this represents the source information the LLM should base its response on
    retrieval_context = [
        """
        Cluster Health Status Context:
        The cluster health analyzer monitors OpenShift cluster components and generates incidents based on firing alerts.
        Current incidents include alerts from namespaces like openshift-monitoring, openshift-cluster-version, and openshift-dns.
        Each incident has a unique UUID identifier and groups related alerts that likely stem from the same root cause.
        The system provides detailed incident information including alert descriptions and overall cluster health summaries.
        Incidents indicate issues with cluster components, node health, and monitoring systems requiring immediate attention.
        """,
        """
        Incident Response Guidelines:
        When queried about cluster status, responses should include:
        - Current number of active incidents
        - Brief description of affected components/namespaces
        - Severity level indicators
        - Incident UUID references for tracking
        - Summary of required actions or attention areas
        """
    ]
    # Create test case for faithfulness evaluation
    test_case = LLMTestCase(
        input=input_query,
        actual_output=actual_output,
        retrieval_context=retrieval_context
    )
    # Assert that the response is faithful to the provided context
    assert_test(test_case, [faithfulness_metric])

def test_answer_correctness():
    input_query = "What is the status of the cluster? Provide a summary of firing incidents if any"
    correctness_metric = GEval(
        name="Correctness",
        criteria="Determine if the 'actual output' is correct based on the 'expected output'.",
        evaluation_params=[LLMTestCaseParams.ACTUAL_OUTPUT, LLMTestCaseParams.EXPECTED_OUTPUT],
        threshold=0.5
    )
    # Craft expected output based on the groupId that should now be firing as an incident
    expected_output = f"Your cluster has multiple current incidents. Each incident has an UUID identifier and includes several alerts affecting one namespace, or more, like openshift-monitoring, openshift-cluster-version, and openshift-dns. These incidents indicate issues with cluster components, node health, and monitoring systems that require attention."
    test_case = LLMTestCase(
        input=input_query,
        actual_output=get_lightspeed_response(input_query),
        expected_output=expected_output,
        retrieval_context=[f"""
         The output must provide detailed incident information, including the incident ID, descriptions of the alerts, and an overall cluster health summary.
         The response should mention specific alert types like TargetDown, ClusterOperatorDown, KubeNodeNotReady, and affected namespaces.
         It is okay if the output provides more detailed information about active incidents even if this goes beyond the expected output.
        """
        ]
    )
    assert_test(test_case, [correctness_metric])

def get_lightspeed_response(query: str) -> str:
    if not LS_API_KEY:
        raise ValueError("LS_API_KEY environment variable is not set")
    
    headers = {
        "accept": "application/json",
        "Content-Type": "application/json",
        "Authorization": f"Bearer {LS_API_KEY}"
    }
    
    data = {"query": query}
    try:
        response = requests.post(URL, headers=headers, json=data, verify=False)
        response.raise_for_status()
        return response.json()["response"]
    except requests.exceptions.RequestException as e:
        raise Exception(f"API request failed: {e}")
